// Copyright 2021 IBM Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package puller

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
	"google.golang.org/grpc/status"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
	"github.com/kserve/modelmesh-runtime-adapter/pullman"
	_ "github.com/kserve/modelmesh-runtime-adapter/pullman/storageproviders/azure"
	_ "github.com/kserve/modelmesh-runtime-adapter/pullman/storageproviders/gcs"
	_ "github.com/kserve/modelmesh-runtime-adapter/pullman/storageproviders/http"
	_ "github.com/kserve/modelmesh-runtime-adapter/pullman/storageproviders/s3"
)

const (
	parameterKeyType  = "type"
	defaultStorageKey = "default"
)

// JSON passed in ModelInfo.Key field of registration requests
type ModelKeyInfo struct {
	// Pass through model_type as-is (it's actually a json object hence interface{} type)
	ModelType     interface{}       `json:"model_type,omitempty"`
	Bucket        string            `json:"bucket,omitempty"`
	DiskSizeBytes int64             `json:"disk_size_bytes"`
	SchemaPath    *string           `json:"schema_path,omitempty"`
	StorageKey    *string           `json:"storage_key,omitempty"`
	StorageParams map[string]string `json:"storage_params,omitempty"`
}

// Puller represents the GRPC server and its configuration
type Puller struct {
	PullerConfig *PullerConfiguration
	Log          logr.Logger
	PullManager  PullerInterface
}

// PullerInterface is the interface for `pullman`
//  useful to mock for testing
type PullerInterface interface {
	Pull(context.Context, pullman.PullCommand) error
}

// NewPuller creates a new Puller instance and initializes it with configuration from the environment
func NewPuller(log logr.Logger) *Puller {
	pullerConfig, err := GetPullerConfigFromEnv(log)
	if err != nil {
		log.Error(err, "Error reading puller configuration from the environment")
		os.Exit(1)
	}
	return NewPullerFromConfig(log, pullerConfig)
}

// NewPullerFromConfig creates a new Puller instance with the given configuration
func NewPullerFromConfig(log logr.Logger, config *PullerConfiguration) *Puller {
	s := new(Puller)
	s.Log = log
	s.PullerConfig = config
	s.PullManager = pullman.NewPullManager(log)

	log.Info("Initializing Puller", "Dir", s.PullerConfig.RootModelDir)

	return s
}

// ProcessLoadModelRequest is for use in an mmesh serving runtime that embeds the puller
//
// The input request is modified in place and also returned.
// After pulling the model files, changes to the request are:
// - rewrite ModelPath to a local filesystem path
// - rewrite ModelKey["schema_path"] to a local filesystem path
// - add the size of the model on disk to ModelKey["disk_size_bytes"]
func (s *Puller) ProcessLoadModelRequest(ctx context.Context, req *mmesh.LoadModelRequest) (*mmesh.LoadModelRequest, error) {
	var modelKey ModelKeyInfo
	if parseErr := json.Unmarshal([]byte(req.ModelKey), &modelKey); parseErr != nil {
		return nil, fmt.Errorf("Invalid modelKey in LoadModelRequest. Error processing JSON '%s': %w", req.ModelKey, parseErr)
	}

	var storageConfig map[string]interface{}
	if modelKey.StorageKey == nil {
		// if storageKey is unspecified, check for a default storage key in the storage config
		storageType := modelKey.StorageParams[parameterKeyType]
		var keyToCheck string
		if storageType == "" {
			keyToCheck = defaultStorageKey
		} else {
			keyToCheck = fmt.Sprintf("%s_%s", defaultStorageKey, storageType)
		}

		var err error
		if storageConfig, err = s.PullerConfig.GetStorageConfiguration(keyToCheck, s.Log); err != nil {
			// do not error here, try to load from the parameters only
			storageConfig = map[string]interface{}{}
		}
	} else {
		var err error
		if storageConfig, err = s.PullerConfig.GetStorageConfiguration(*modelKey.StorageKey, s.Log); err != nil {
			// if key was specified, but we could not find it, that is an error
			return nil, fmt.Errorf("Did not find storage config for key %s: %w", *modelKey.StorageKey, err)
		}
	}

	// DEPRECATED: allow top-level bucket key for backwards compatibility
	if modelKey.Bucket != "" && storageConfig["bucket"] != nil {
		s.Log.Info(`Warning: use of ModelKey["bucket"] is deprecated, use ModelKey["storage_params"]["bucket"] instead`)
		storageConfig["bucket"] = modelKey.Bucket
	}

	// override the storage configs with per-request storage parameters
	if err := ApplyParameterOverrides(storageConfig, modelKey.StorageParams); err != nil {
		return nil, fmt.Errorf("Unable to merge storage parameters from the storage config and the Predictor Storage field: %w", err)
	}

	// if we still don't know the storage type, we cannot download the model, so return an error
	storageType, ok := storageConfig[parameterKeyType].(string)
	if !ok {
		return nil, fmt.Errorf("Predictor Storage field missing")
	}

	// build and execute the pull command

	// name the local files based on the last element of the paths
	// TODO: should have some sanitization for filenames
	modelPathFilename := filepath.Base(req.ModelPath)
	targets := []pullman.Target{
		{
			RemotePath: req.ModelPath,
			LocalPath:  modelPathFilename,
		},
	}

	// if included, add the schema to the pull
	var schemaPathFilename string
	if modelKey.SchemaPath != nil {
		schemaPath := *modelKey.SchemaPath
		schemaPathFilename = filepath.Base(schemaPath)
		// if the names match, generate an internal name
		if modelPathFilename == schemaPathFilename {
			schemaPathFilename = "_schema.json"
		}

		schemaTarget := pullman.Target{
			RemotePath: schemaPath,
			LocalPath:  schemaPathFilename,
		}
		targets = append(targets, schemaTarget)
	}

	modelDir, joinErr := util.SecureJoin(s.PullerConfig.RootModelDir, req.ModelId)
	if joinErr != nil {
		return nil, fmt.Errorf("Error joining paths '%s' and '%s': %v", s.PullerConfig.RootModelDir, req.ModelId, joinErr)
	}

	pullCommand := pullman.PullCommand{
		RepositoryConfig: pullman.NewRepositoryConfig(storageType, storageConfig),
		Directory:        modelDir,
		Targets:          targets,
	}
	pullerErr := s.PullManager.Pull(ctx, pullCommand)
	if pullerErr != nil {
		return nil, status.Errorf(status.Code(pullerErr), "Failed to pull model from storage due to error: %s", pullerErr)
	}

	// update model path to an absolute path in the local filesystem
	modelFullPath, joinErr := util.SecureJoin(modelDir, modelPathFilename)
	if joinErr != nil {
		return nil, fmt.Errorf("Error joining paths '%s' and '%s': %w", modelDir, modelPathFilename, joinErr)
	}
	req.ModelPath = modelFullPath

	// if it was included, update schema path to an absolute path in the local filesystem
	if modelKey.SchemaPath != nil {
		schemaFullPath, joinErr := util.SecureJoin(modelDir, schemaPathFilename)
		if joinErr != nil {
			return nil, fmt.Errorf("Error joining paths '%s' and '%s': %w", modelDir, schemaPathFilename, joinErr)
		}
		modelKey.SchemaPath = &schemaFullPath
	}

	// update the model key to add the disk size
	if size, err1 := getModelDiskSize(modelFullPath); err1 != nil {
		s.Log.Error(err1, "Model disk size will not be included in the LoadModelRequest due to error", "model_key", modelKey)
	} else {
		modelKey.DiskSizeBytes = size
	}

	// Clear storage parameters from the processed modelKey
	modelKey.StorageKey = nil
	modelKey.StorageParams = nil
	modelKey.Bucket = ""

	// rewrite the ModelKey JSON with any updates that have been made
	modelKeyBytes, err := json.Marshal(modelKey)
	if err != nil {
		return nil, fmt.Errorf("error serializing ModelKey back to JSON: %w", err)
	}
	req.ModelKey = string(modelKeyBytes)

	return req, nil
}

func getModelDiskSize(modelPath string) (int64, error) {
	// This walks the local filesystem and accumulates the size of the model
	// It would be more efficient to accumulate the size as the files are downloaded,
	// but this would require refactoring because the s3 download iterator does not return a size.
	var size int64
	err := filepath.Walk(modelPath, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	if err != nil {
		return size, fmt.Errorf("Error computing model's disk size: %w", err)
	}

	return size, nil
}

func (p *Puller) CleanupModel(modelID string) error {
	// Now delete the local file
	pathToModel, err := util.SecureJoin(p.PullerConfig.RootModelDir, modelID)
	if err != nil {
		p.Log.Error(err, "Error joining paths", "RootModelDir", p.PullerConfig.RootModelDir, "ModelId", modelID)
		return err
	}
	if err = os.RemoveAll(pathToModel); err != nil {
		p.Log.Error(err, "Model unload failed to delete files from the local filesystem", "local_dir", pathToModel)
		return fmt.Errorf("Failed to delete model from local filesystem: %w", err)
	}
	return nil
}

func (p *Puller) ClearLocalModelStorage(exclude string) error {
	return util.ClearDirectoryContents(p.PullerConfig.RootModelDir, func(f os.DirEntry) bool {
		return f.Name() != exclude
	})
}

func (p *Puller) ListModels() ([]string, error) {
	entries, err := ioutil.ReadDir(p.PullerConfig.RootModelDir)
	if err != nil {
		return nil, err
	}
	var results = make([]string, len(entries))
	for i, entry := range entries {
		results[i] = entry.Name()
	}
	return results, nil
}
