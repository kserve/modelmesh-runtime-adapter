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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

// The OVMS Model Manager follows the Actor pattern to own and manage models
//
// The exposed functions are thread safe; they send messages to the Actor and
// wait for a response.
// The Manager runs a background event loop to process requests
type OvmsModelManager struct {
	modelConfigFilename string
	log                 logr.Logger

	address string
	client  *http.Client

	loadedModelsMap           map[string]OvmsMultiModelConfigListEntry
	cachedModelConfigResponse OvmsConfigResponse
	mux                       sync.Mutex
}

func NewOvmsModelManager(address string, configFilename string, log logr.Logger) *OvmsModelManager {

	// load initial config from disk, if it exists
	// this handles the case where the adapter crashes
	multiModelConfig := map[string]OvmsMultiModelConfigListEntry{}
	if configBytes, err := os.ReadFile(configFilename); err != nil {
		// if there is any error in initialization from an existing file, just continue with an empty config
		// but log if there was an error reading an exsting file
		if !errors.Is(err, os.ErrNotExist) {
			log.Error(err, "WARNING: could not initialize model config from file, will contine with empty config", "filename", configFilename)
		}
	} else {
		var modelRepositoryConfig OvmsMultiModelRepositoryConfig
		if err := json.Unmarshal(configBytes, &modelRepositoryConfig); err != nil {
			log.Error(err, "WARNING: could not parse model repository JSON, will contine with empty config", "filename", configFilename)
		} else {
			multiModelConfig = make(map[string]OvmsMultiModelConfigListEntry, len(modelRepositoryConfig.ModelConfigList))
			for _, mc := range modelRepositoryConfig.ModelConfigList {
				multiModelConfig[mc.Config.Name] = mc
			}
		}
	}

	ovmsMM := &OvmsModelManager{
		address:             address,
		modelConfigFilename: configFilename,
		log:                 log,
		loadedModelsMap:     multiModelConfig,
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxConnsPerHost:     100,
				MaxIdleConnsPerHost: 100,
			},
			Timeout: 30 * time.Second,
		},

		// will need to be updated before being queried
		cachedModelConfigResponse: OvmsConfigResponse{},
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

	if err := mm.updateModelConfig(ctx); err != nil {
		return err
	}

	// check the status of the model to see if it is loaded
	mvs, ok := mm.cachedModelConfigResponse[modelId]
	if !ok || len(mvs.ModelVersionStatus) == 0 {
		// this shouldn't really ever happen
		return fmt.Errorf("OVMS model load failed, model not found in cache after the reload")
	}
	//  we will only ever load a single version of a model, hence [0]
	status := mvs.ModelVersionStatus[0]
	if status.State == "AVAILABLE" {
		return nil
	}

	return fmt.Errorf("OVMS model load failed. code: '%s' reason: '%s'", status.Status.ErrorCode, status.Status.ErrorMessage)

}

func (mm *OvmsModelManager) UnloadModel(ctx context.Context, modelId string) error {
	mm.mux.Lock()
	defer mm.mux.Unlock()

	delete(mm.loadedModelsMap, modelId)

	if err := mm.updateModelConfig(ctx); err != nil {
		return err
	}

	return nil
}

func (mm *OvmsModelManager) UnloadAll(ctx context.Context) error {
	mm.mux.Lock()
	defer mm.mux.Unlock()

	// reset the repo config to an empty list
	mm.loadedModelsMap = map[string]OvmsMultiModelConfigListEntry{}

	return mm.updateModelConfig(context.Background())
}

func (mm *OvmsModelManager) GetConfig(ctx context.Context) error {
	mm.mux.Lock()
	defer mm.mux.Unlock()

	return mm.getConfig(ctx)
}

// internal functions

func (mm *OvmsModelManager) getConfig(ctx context.Context) error {
	//NB: assumes the mutex is locked

	// query the Config Status API
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/v1/config", mm.address), http.NoBody)
	if err != nil {
		// this should never happen...
		return fmt.Errorf("Error constructing config status request: %w", err)
	}

	resp, err := mm.client.Do(req)
	if err != nil {
		// TODO: check if error is a timeout and handle appropriately
		return fmt.Errorf("Protocol error getting the config: %w", err)
	}
	defer resp.Body.Close()

	// read the body
	// NOTE: if the body is not read, the connection cannot be re-used, so
	// we read the body regardless of the status of the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		// TODO: Error returned should be a gRPC error code?
		return fmt.Errorf("Error reading config status response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Error getting config status: %w", err)
	}

	var c OvmsConfigResponse
	if err := json.Unmarshal(body, &c); err != nil {
		return fmt.Errorf("Error parsing config status response: %w", err)
	}
	mm.cachedModelConfigResponse = c

	return nil
}

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

	if err := ioutil.WriteFile(mm.modelConfigFilename, modelRepositoryConfigJSON, 0644); err != nil {
		return fmt.Errorf("Error writing config file: %w", err)
	}

	return nil
}

// updateModelConfig updates the model configuration for OVMS
//
// An error is returned if the configuration reload was not confirmed
// As part of the update, cachedModelConfigResponse is updated.
func (mm *OvmsModelManager) updateModelConfig(ctx context.Context) error {
	// NB: assumes mutex is locked!

	if err := mm.writeConfig(); err != nil {
		return fmt.Errorf("Error updating model config when writing config file: %w", err)
	}

	// Send config reload request to OVMS
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/v1/config/reload", mm.address), http.NoBody)
	if err != nil {
		// this should never happen...
		return fmt.Errorf("Error constructing reload config request: %w", err)
	}

	// Handling of the response
	// - If connection error: return the error
	// - If timeout? Maybe should have retries
	// - If response is 201 or 200: parse and cache the model config
	// - If other HTTP error, check error message in JSON
	//    If model load error: query the config status API
	//    If other error, just return it?
	resp, err := mm.client.Do(req)
	if err != nil {
		// TODO: check if error is a timeout and handle appropriately
		return fmt.Errorf("Protocol error reloading config: %w", err)
	}
	defer resp.Body.Close()

	// read the body
	// NOTE: if the body is not read, the connection cannot be re-used, so
	// we ready the body regardless of the status of the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		// TODO: Error returned should be a gRPC error code?
		return fmt.Errorf("Error reading config reload response body: %w", err)
	}

	// Successful config reload
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		var c OvmsConfigResponse
		if err = json.Unmarshal(body, &c); err != nil {
			return fmt.Errorf("Error parsing config reload response: %w", err)
		}
		mm.cachedModelConfigResponse = c

		return nil
	}

	// Config reload failed, try to figure out why
	//   OVMS REST response documentation: https://github.com/openvinotoolkit/model_server/blob/main/docs/model_server_rest_api.md#config-reload-api-
	var errorResponse struct{ errorDescription string }
	if err = json.Unmarshal(body, &errorResponse); err != nil {
		return fmt.Errorf("Error parsing model config error response: %w", err)
	}

	// Handle an error loading one or more of the models
	// The response will not include the model statuses, but we can query
	// for the config separately to get details on the failing models
	if strings.HasPrefix(errorResponse.errorDescription, "Reloading models versions failed") {
		if err2 := mm.getConfig(ctx); err2 != nil {
			return err2
		}

		// getConfig updates cachedModelConfigResponse

		return nil
	}

	return fmt.Errorf("Unhandled error reloading config: %w", err)
}
