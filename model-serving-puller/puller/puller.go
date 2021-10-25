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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"google.golang.org/grpc/status"

	"github.com/kserve/modelmesh-runtime-adapter/internal/modelschema"
	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
)

const jsonAttrModelKeyStorageKey = "storage_key"
const jsonAttrModelKeyBucket = "bucket"
const jsonAttrModelKeyDiskSizeBytes = "disk_size_bytes"
const jsonAttrModelSchemaPath = "schema_path"
const jsonAttrStorageParams = "storage_params"

// Puller represents the GRPC server and its configuration
type Puller struct {
	PullerConfig              *PullerConfiguration
	Log                       logr.Logger
	NewS3DownloaderFromConfig func(*StorageConfiguration, int, logr.Logger) (S3Downloader, error)
	s3DownloaderCache         map[string]S3Downloader
	mutex                     sync.RWMutex
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
	s.NewS3DownloaderFromConfig = NewS3Downloader
	s.s3DownloaderCache = make(map[string]S3Downloader)

	log.Info("Initializing Puller", "Dir", s.PullerConfig.RootModelDir)

	s.startCleanCacheTicker(s.PullerConfig.CacheCleanPeriod)

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
	// parse json
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

	//  get storage config
	storageConfig, err := s.PullerConfig.GetStorageConfiguration(storageKey, s.Log)
	if err != nil {
		return nil, err
	}

	//  get storage params
	storageParams, ok := modelKey[jsonAttrStorageParams].(map[string]interface{})
	if !ok {
		// backwards compatability: if storage_params does not exist
		bucketName, ok := modelKey[jsonAttrModelKeyBucket].(string)
		if !ok {
			if modelKey[jsonAttrModelKeyBucket] != nil {
				return nil, fmt.Errorf("Invalid modelKey in LoadModelRequest, '%s' attribute must have a string value. Found value %v", jsonAttrModelKeyBucket, modelKey[jsonAttrModelKeyBucket])
			}
		}
		storageParams = make(map[string]interface{})
		storageParams[jsonAttrModelKeyBucket] = bucketName
		modelKey[jsonAttrStorageParams] = storageParams
	}

	// download the model
	localPath, pullerErr := s.DownloadFromCOS(req.ModelId, req.ModelPath, schemaPath, storageKey, storageConfig, storageParams)
	if pullerErr != nil {
		return nil, status.Errorf(status.Code(pullerErr), "Failed to pull model from storage due to error: %s", pullerErr)
	}

	// update the model path
	req.ModelPath = localPath

	// update the model key to add the schema path
	if schemaPath != "" {
		schemaFullPath, joinErr := util.SecureJoin(s.PullerConfig.RootModelDir, req.ModelId, modelschema.ModelSchemaFile)
		if joinErr != nil {
			return nil, fmt.Errorf("Error joining paths '%s', '%s', and '%s': %w", s.PullerConfig.RootModelDir, req.ModelId, modelschema.ModelSchemaFile, joinErr)
		}
		modelKey[jsonAttrModelSchemaPath] = schemaFullPath
	}

	// update the model key to add the disk size
	if size, err1 := getModelDiskSize(localPath); err1 != nil {
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

func (s *Puller) startCleanCacheTicker(d time.Duration) {
	// NewTicker panics if passed a non-positive duration
	// this also gives a way to disable the cache cleaning
	if d <= 0 {
		return
	}
	ticker := time.NewTicker(d)
	go func() {
		for range ticker.C {
			s.CleanCache()
		}
	}()
}

func (s *Puller) CleanCache() {
	s.Log.Info("Running s3 downloader cache cleanup")
	for storageKey := range s.s3DownloaderCache {
		var err error
		configPath, err := util.SecureJoin(s.PullerConfig.StorageConfigurationDir, storageKey)
		if err != nil {
			s.Log.Error(err, "Error deleting downloader from cache, nothing was removed", "storage_key", storageKey)
			return
		}
		_, err = os.Stat(configPath)
		if os.IsNotExist(err) {
			s.mutex.Lock()
			s.Log.Info("Deleting downloader from cache", "storage_key", storageKey)
			delete(s.s3DownloaderCache, storageKey)
			s.mutex.Unlock()
		}
	}
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
