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
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

type OvmsModelManager struct {
	address             string
	modelConfigFilename string
	log                 logr.Logger

	loadedModelsMap map[string]OvmsMultiModelConfigListEntry
	client          *http.Client
	mux             sync.Mutex
}

func NewOvmsModelManager(address string, configFilename string, log logr.Logger) *OvmsModelManager {

	ovmsMM := &OvmsModelManager{
		address:             address,
		modelConfigFilename: configFilename,
		log:                 log,
		// TODO: On boot, should construct Multi-Model config from on-disk files in case the adapter crashes
		loadedModelsMap: map[string]OvmsMultiModelConfigListEntry{},
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxConnsPerHost:     100,
				MaxIdleConnsPerHost: 100,
			},
			Timeout: 30 * time.Second,
		},
	}

	// write the config out on boot because OVMS needs it to exist
	if _, err := os.Stat(configFilename); os.IsNotExist(err) {
		if err = ovmsMM.writeConfig(); err != nil {
			log.Error(err, "Unable to write out empty config file")
		}
	}

	return ovmsMM
}

// "Client" API
func (mm *OvmsModelManager) LoadModel(ctx context.Context, modelPath string, modelId string) error {
	mm.mux.Lock()
	defer mm.mux.Unlock()

	// BasePath must be a directory
	var basePath string
	if fileInfo, err := os.Stat(modelPath); err == nil {
		if fileInfo.IsDir() {
			basePath = modelPath
		} else {
			basePath = filepath.Dir(modelPath)
		}
	} else {
		return fmt.Errorf("Could not stat file at the model_path: %w", err)

	}

	mm.loadedModelsMap[modelId] = OvmsMultiModelConfigListEntry{
		Config: OvmsMultiModelModelConfig{
			Name:     modelId,
			BasePath: basePath,
		},
	}

	if err := mm.reloadConfig(ctx); err != nil {
		return err
	}

	return nil
}

func (mm *OvmsModelManager) UnloadModel(ctx context.Context, modelId string) error {
	mm.mux.Lock()
	defer mm.mux.Unlock()

	delete(mm.loadedModelsMap, modelId)

	if err := mm.reloadConfig(ctx); err != nil {
		return err
	}

	return nil
}

func (mm *OvmsModelManager) UnloadAll() error {
	// reset the repo config to an empty list
	mm.loadedModelsMap = map[string]OvmsMultiModelConfigListEntry{}

	return mm.reloadConfig(context.Background())
}

func (mm *OvmsModelManager) GetConfig(ctx context.Context) (interface{}, error) {
	// Send config reload request to OVMS
	// TODO: some basic retries?
	resp, err := mm.client.Get(fmt.Sprintf("%s/v1/config", mm.address))
	if err != nil {
		// TODO: Error returned should be a gRPC error code?
		return nil, fmt.Errorf("Error reloading config: %w", err)
	}
	// handle the response body
	defer resp.Body.Close()
	io.Copy(ioutil.Discard, resp.Body)

	return nil, nil
}

// internal functions
func (mm *OvmsModelManager) writeConfig() error {
	// NB: assumes mutex is locked!

	// Build the model reposritory config to be written out
	modelRepositoryConfig := OvmsMultiModelRepositoryConfig{
		ModelConfigList: make([]OvmsMultiModelConfigListEntry, len(mm.loadedModelsMap)),
	}
	listIndex := 0
	for _, model := range mm.loadedModelsMap {
		modelRepositoryConfig.ModelConfigList[listIndex] = model
		listIndex++
	}

	modelRepositoryConfigJSON, err := json.Marshal(modelRepositoryConfig)
	if err != nil {
		return fmt.Errorf("Error marshalling config file: %w", err)
	}

	if err = ioutil.WriteFile(mm.modelConfigFilename, modelRepositoryConfigJSON, 0644); err != nil {
		return fmt.Errorf("Error writing config file: %w", err)
	}

	return nil
}

// reloadConfig triggers OVMS to reload
// An error is returned if:
// - the config fails the schema check
// - if ANY of the configured models fail to load
func (mm *OvmsModelManager) reloadConfig(ctx context.Context) error {
	// NB: assumes mutex is locked!

	mm.writeConfig()

	// Send config reload request to OVMS
	resp, err := mm.client.Post(fmt.Sprintf("%s/v1/config/reload", mm.address), "", nil)
	if err != nil {
		// TODO: Error returned should be a gRPC error code?
		return fmt.Errorf("Error reloading config: %w", err)
	}
	// handle the response body by just reading and discarding it
	// NOTE: if the body is not read, the connection cannot be re-used
	defer resp.Body.Close()
	io.Copy(ioutil.Discard, resp.Body)

	return nil
}
