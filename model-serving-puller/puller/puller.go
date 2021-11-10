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
	_ "github.com/kserve/modelmesh-runtime-adapter/pullman/storageproviders/s3"
)

const jsonAttrModelKeyStorageKey = "storage_key"
const jsonAttrModelKeyBucket = "bucket"
const jsonAttrModelKeyDiskSizeBytes = "disk_size_bytes"
const jsonAttrModelSchemaPath = "schema_path"
const jsonAttrStorageParams = "storage_params"

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
func (s *Puller) ProcessLoadModelRequest(req *mmesh.LoadModelRequest) (*mmesh.LoadModelRequest, error) {
	var modelKey map[string]interface{}
	if parseErr := json.Unmarshal([]byte(req.ModelKey), &modelKey); parseErr != nil {
		return nil, fmt.Errorf("Invalid modelKey in LoadModelRequest. ModelKey value '%s' is not valid JSON: %s", req.ModelKey, parseErr)
	}
	schemaPath, ok := modelKey[jsonAttrModelSchemaPath].(string)
	if !ok {
		if modelKey[jsonAttrModelSchemaPath] != nil {
			return nil, fmt.Errorf("Invalid schemaPath in LoadModelRequest, '%s' attribute must have a string value. Found value %v", jsonAttrModelSchemaPath, modelKey[jsonAttrModelSchemaPath])
		}
	}
	storageKey, ok := modelKey[jsonAttrModelKeyStorageKey].(string)
	if !ok {
		return nil, fmt.Errorf("Predictor Storage field missing")
	}

	storageConfig, err := s.PullerConfig.GetStorageConfiguration(storageKey, s.Log)
	if err != nil {
		return nil, err
	}

	// override storage config with per-request parameters
	//  check "storage_params" first, but also check the top-level "bucket" key if
	//  "storage_params" doesn't exist for backwards compatibility
	if storageParams, ok := modelKey[jsonAttrStorageParams].(map[string]interface{}); ok {
		if bucketName, ok1 := storageParams[jsonAttrModelKeyBucket]; ok1 {
			storageConfig.Set("bucket", bucketName)
		}
	} else if bucketName, ok := modelKey[jsonAttrModelKeyBucket]; ok {
		// return error if key exists but it is not a string
		if _, ok1 := bucketName.(string); !ok1 {
			return nil, fmt.Errorf("Invalid modelKey in LoadModelRequest, '%s' attribute must have a string value. Found value %v", jsonAttrModelKeyBucket, modelKey[jsonAttrModelKeyBucket])
		} else {
			storageConfig.Set("bucket", bucketName)
		}
	}

	modelDir, joinErr := util.SecureJoin(s.PullerConfig.RootModelDir, req.ModelId)
	if joinErr != nil {
		return nil, fmt.Errorf("Error joining paths '%s' and '%s': %v", s.PullerConfig.RootModelDir, req.ModelId, joinErr)
	}

	// build and execute the pull command

	// name the local files based on the last element of the paths
	// TODO: should have some sanitization for filenames
	modelPathFilename := filepath.Base(req.ModelPath)
	schemaPathFilename := filepath.Base(schemaPath)
	if modelPathFilename == schemaPathFilename {
		schemaPathFilename = "_schema.json"
	}

	targets := []pullman.Target{
		{
			RemotePath: req.ModelPath,
			LocalPath:  modelPathFilename,
		},
	}
	if schemaPath != "" {
		schemaTarget := pullman.Target{
			RemotePath: schemaPath,
			LocalPath:  schemaPathFilename,
		}
		targets = append(targets, schemaTarget)
	}

	pullCommand := pullman.PullCommand{
		RepositoryConfig: storageConfig,
		Directory:        modelDir,
		Targets:          targets,
	}
	pullerErr := s.PullManager.Pull(context.TODO(), pullCommand)
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
	if schemaPath != "" {
		schemaFullPath, joinErr := util.SecureJoin(modelDir, schemaPathFilename)
		if joinErr != nil {
			return nil, fmt.Errorf("Error joining paths '%s' and '%s': %w", modelDir, schemaPathFilename, joinErr)
		}
		modelKey[jsonAttrModelSchemaPath] = schemaFullPath
	}

	// update the model key to add the disk size
	if size, err1 := getModelDiskSize(modelFullPath); err1 != nil {
		s.Log.Info("Model disk size will not be included in the LoadModelRequest due to error", "model_key", modelKey, "error", err1)
	} else {
		modelKey[jsonAttrModelKeyDiskSizeBytes] = size
	}

	// rewrite the ModelKey JSON with any updates that have been made
	modelKeyBytes, err := json.Marshal(modelKey)
	if err != nil {
		return nil, fmt.Errorf("Error serializing ModelKey back to JSON: %w", err)
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
	err = os.RemoveAll(pathToModel)
	if err != nil {
		p.Log.Error(err, "Model unload failed to delete files from the local filesystem", "local_dir", pathToModel)
		return fmt.Errorf("Failed to delete model from local filesystem: %w", err)
	}
	return nil
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
