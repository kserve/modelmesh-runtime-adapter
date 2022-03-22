// Copyright 2022 IBM Corporation
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
	"time"

	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The OVMS Model Manager follows the Actor pattern to own and manage models
//
// The exposed functions are thread safe; they send messages to the Actor and
// wait for a response. The Manager runs a background event loop to process
// model updates in batches
type OvmsModelManager struct {
	modelConfigFilename string
	log                 logr.Logger
	address             string

	cachedModelConfigResponse OvmsConfigResponse
	client                    *http.Client
	loadedModelsMap           map[string]OvmsMultiModelConfigListEntry
	requests                  chan *request
}

func NewOvmsModelManager(address string, configFilename string, log logr.Logger) *OvmsModelManager {

	// load initial config from disk, if it exists
	// this handles the case where the adapter crashes
	multiModelConfig := map[string]OvmsMultiModelConfigListEntry{}
	if configBytes, err := os.ReadFile(configFilename); err != nil {
		// if there is any error in initialization from an existing file, just continue with an empty config
		// but log if there was an error reading an existing file
		if !errors.Is(err, os.ErrNotExist) {
			log.Error(err, "WARNING: could not initialize model config from file, will contine with empty config", "filename", configFilename)
		}
	} else {
		var modelRepositoryConfig OvmsMultiModelRepositoryConfig
		if err := json.Unmarshal(configBytes, &modelRepositoryConfig); err != nil {
			log.Error(err, "WARNING: could not parse model config JSON, will contine with empty config", "filename", configFilename)
		} else {
			multiModelConfig = make(map[string]OvmsMultiModelConfigListEntry, len(modelRepositoryConfig.ModelConfigList))
			for _, mc := range modelRepositoryConfig.ModelConfigList {
				multiModelConfig[mc.Config.Name] = mc
			}
		}
	}

	ovmsMM := &OvmsModelManager{
		address: address,
		// will need to be updated before being queried
		cachedModelConfigResponse: OvmsConfigResponse{},
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxConnsPerHost:     100,
				MaxIdleConnsPerHost: 100,
			},
			Timeout: 30 * time.Second,
		},
		log:                 log,
		loadedModelsMap:     multiModelConfig,
		modelConfigFilename: configFilename,
		requests:            make(chan *request, 25), // TODO make configurable
	}

	// write the config out on boot because OVMS needs it to exist
	if _, err := os.Stat(configFilename); os.IsNotExist(err) {
		if err = ovmsMM.writeConfig(); err != nil {
			log.Error(err, "Unable to write out empty config file")
		}
	}

	// start the actor process
	go ovmsMM.run()

	return ovmsMM
}

// TODO?
// func (mm *OvmsModelManager) Close() {
// 	close(mm.reqs)
// }

// "Client" API
func (mm *OvmsModelManager) LoadModel(ctx context.Context, modelPath string, modelId string) error {

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

	c := make(chan error, 1)
	mm.requests <- &request{
		requestType: load,
		modelId:     modelId,
		basePath:    basePath,
		c:           c,
		ctx:         ctx,
	}

	select {
	case err := <-c:
		if err != nil {
			return fmt.Errorf("LoadModel failed: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("LoadModel request was cancelled")
	}
}

func (mm *OvmsModelManager) UnloadModel(ctx context.Context, modelId string) error {
	c := make(chan error, 1)
	mm.requests <- &request{
		requestType: unload,
		modelId:     modelId,
		c:           c,
		ctx:         ctx,
	}

	select {
	case err := <-c:
		if err != nil {
			return fmt.Errorf("UnloadModel failed: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("UnloadModel request was cancelled")
	}
}

func (mm *OvmsModelManager) UnloadAll(ctx context.Context) error {
	c := make(chan error, 1)
	mm.requests <- &request{
		requestType: unloadAll,
		c:           c,
		ctx:         ctx,
	}

	select {
	case err := <-c:
		if err != nil {
			return fmt.Errorf("UnloadAll failed: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("UnloadAll request was cancelled")
	}
}

func (mm *OvmsModelManager) GetConfig(ctx context.Context) error {
	return mm.getConfig(ctx)
}

// internal

type requestType int64

const (
	load requestType = iota
	unload
	unloadAll
)

type request struct {
	requestType requestType

	modelId  string // for load and unload
	basePath string // for load

	ctx context.Context
	c   chan<- error
}

// Run loop for the manager's internal actor that owns the model repository config
//
//
// Maintains a slice of batched requests that are in process in the reload
//
// Returns results from the reload operation once it completes
// Receives a stream of requests from its channel
func (mm *OvmsModelManager) run() {
	log := mm.log.WithValues("thread", "run")
	log.Info("Starting ModelManger thread")
	for mm.requests != nil {
		// read a batch of requests to process
		// we need a criteria to determine when to stop batching
		// requests; here we take the approach of waiting for a fixed
		// period of time after a request is received
		batch := make([]*request, 0, 10)
		var stopChan <-chan time.Time

	runLoopSelect:
		select {
		case req, ok := <-mm.requests:
			if !ok {
				// shutdown the loop after processing this batch
				mm.requests = nil
				break // from select
			}
			// add request to batch
			batch = append(batch, req)

			// set the stop timer, if not set already
			if stopChan == nil {
				stopChan = time.NewTimer(100 * time.Millisecond).C
			}

			goto runLoopSelect
		case <-stopChan:
			break // from select
		}

		log.V(1).Info("Processing batch of requests", "numRequests", len(batch))

		// update the model configuration based on the requests
		var unloadAllRequest *request

		// build a new map to track changes to the same model
		modelUpdates := map[string]*request{}
		for _, req := range batch {
			// check for an unloadAll
			if req.requestType == unloadAll {
				unloadAllRequest = req
				// abort any requests preceding the unloadAll
				for id, req := range modelUpdates {
					completeRequest(req, codes.Aborted, "Model Server is being reset")
					// remove the pointer from the updates map
					delete(modelUpdates, id)
				}
				continue
			}

			if modelUpdates[req.modelId] != nil {
				// abort the prior request first
				completeRequest(modelUpdates[req.modelId], codes.Aborted, "Concurrent request received for the same model")
			}

			modelUpdates[req.modelId] = req
		}

		// if the batch included an UnloadAll, reset the model config map prior adding the updates
		if unloadAllRequest != nil {
			mm.loadedModelsMap = map[string]OvmsMultiModelConfigListEntry{}
		}

		// process the updates
		for id, req := range modelUpdates {
			switch req.requestType {
			case load:
				mm.loadedModelsMap[id] = OvmsMultiModelConfigListEntry{
					Config: OvmsMultiModelModelConfig{
						Name:     req.modelId,
						BasePath: req.basePath,
					},
				}
			case unload:
				delete(mm.loadedModelsMap, id)
			}
		}

		// reload the config
		ctx := context.TODO()
		if err := mm.updateModelConfig(ctx); err != nil {
			msg := "Failed to update model configuration with OVMS"
			log.Error(err, msg)

			// at this point, we don't know whether OVMS has
			// reloaded or not... so complete all requests with
			// errors

			for _, req := range modelUpdates {
				completeRequest(req, codes.Internal, msg)
			}

			if unloadAllRequest != nil {
				completeRequest(unloadAllRequest, codes.Internal, msg)
			}

			continue // back to the start of the main loop
		}

		// complete the requests
		for id, req := range modelUpdates {
			var statusExists bool
			var modelStatus OvmsModelVersionStatus

			conf, statusExists := mm.cachedModelConfigResponse[id]
			if statusExists {
				modelStatus = conf.ModelVersionStatus[0]
			}

			switch req.requestType {
			case load:
				if !statusExists {
					completeRequest(req, codes.Internal, "Expected model to load, but no status entry found")
				} else if modelStatus.State == "AVAILABLE" {
					completeRequest(req, codes.OK, "")
				} else {
					completeRequest(req, codes.InvalidArgument, fmt.Sprintf("OVMS model load failed. code: '%s' reason: '%s'", modelStatus.Status.ErrorCode, modelStatus.Status.ErrorMessage))
				}
			case unload:
				if !statusExists {
					// OVMS keeps a reference to a loaded model that is unloaded, so this case means that the model
					// was not loaded previously. This case is unexpected, but we can proceed
					log.Info("Processed UnloadModel for model that was never loaded", "modelId", req.modelId)
					completeRequest(req, codes.OK, "")
				} else if modelStatus.State == "END" {
					completeRequest(req, codes.OK, "")
				} else {
					completeRequest(req, codes.Internal, fmt.Sprintf("OVMS model unload failed. state: %s", modelStatus.State))
				}
			}
		}

		// as long as the reload was attempted, assume that the unloadAll completed successfully
		if unloadAllRequest != nil {
			completeRequest(unloadAllRequest, codes.OK, "")
		}

	}

	log.Info("ModelManager thread exiting")
}

func completeRequest(req *request, code codes.Code, reason string) {
	// if code == OK, status.Error returns nil
	req.c <- status.Error(code, reason)
}

func (mm *OvmsModelManager) getConfig(ctx context.Context) error {
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
		return fmt.Errorf("Error reading config status response body: %w", err)
	}

	// handle successful request
	if resp.StatusCode == http.StatusOK {
		var c OvmsConfigResponse
		if err1 := json.Unmarshal(body, &c); err1 != nil {
			return fmt.Errorf("Error parsing config status response: %w", err1)
		}
		mm.cachedModelConfigResponse = c

		return nil
	}

	var errorResponse OvmsConfigErrorResponse
	if err = json.Unmarshal(body, &errorResponse); err != nil {
		return fmt.Errorf("Error parsing model config error response: %w", err)
	}

	errDesc := fmt.Errorf("Error response when getting the config: %s", errorResponse.Error)
	mm.log.Error(errDesc, "Call to /v1/config returned an error", "code", resp.StatusCode)

	return status.Error(codes.Internal, errDesc.Error())
}

func (mm *OvmsModelManager) writeConfig() error {
	// Build the model repository config to be written out
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
// An error is returned if the reload was not confirmed to be completed; the
// error will be nil even if a model load fails.
//
// The returned config is saved to cachedModelConfigResponse.
func (mm *OvmsModelManager) updateModelConfig(ctx context.Context) error {
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
	// - If timeout: Maybe should have retries?
	// - If response is 201 or 200: parse and cache the model config
	// - If other HTTP error, check error message in JSON
	//    If model load error: query the config status API
	//    If other error, just return it?
	resp, err := mm.client.Do(req)
	if err != nil {
		// TODO: check if error is a timeout and handle appropriately
		return fmt.Errorf("Communication error reloading the config: %w", err)
	}
	defer resp.Body.Close()

	// read the body
	// NOTE: if the body is not read, the connection cannot be re-used, so
	// we read the body regardless of the status of the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
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
	// The response will not include the model statuses, but we can query
	// for the config separately to get details on the failing models
	var errorResponse OvmsConfigErrorResponse
	if err = json.Unmarshal(body, &errorResponse); err != nil {
		return fmt.Errorf("Error parsing model config error response: %w", err)
	}

	mm.log.Error(fmt.Errorf("Error response when reloading the config: %s", errorResponse.Error), "Call to /v1/config/reload returned an error", "code", resp.StatusCode)

	// we rely on the fact that getConfig updates cachedModelConfigResponse
	return mm.getConfig(ctx)
}
