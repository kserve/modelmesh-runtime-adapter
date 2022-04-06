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
	"io/fs"
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
	config              ModelManagerConfig

	// internal
	cachedModelConfigResponse OvmsConfigResponse
	client                    *http.Client
	loadedModelsMap           map[string]OvmsMultiModelConfigListEntry
	requests                  chan *request

	// optimizations
	// keep reference to temporary map to avoid re-allocating arrays each
	// time the config is written out
	modelRepositoryConfigList []OvmsMultiModelConfigListEntry
	// build HTTP requests once and re-use them (this is safe only because
	// they do not have a body)
	configRequest *http.Request
	reloadRequest *http.Request
}

type ModelManagerConfig struct {
	BatchWaitTimeMin time.Duration
	BatchWaitTimeMax time.Duration

	HttpClientMaxConns int
	ReloadTimeout      time.Duration

	ModelConfigFilePerms fs.FileMode

	RequestChannelSize int
}

var modelManagerConfigDefaults ModelManagerConfig = ModelManagerConfig{
	BatchWaitTimeMin:     100 * time.Millisecond,
	BatchWaitTimeMax:     3 * time.Second,
	HttpClientMaxConns:   100,
	ReloadTimeout:        30 * time.Second,
	RequestChannelSize:   25,
	ModelConfigFilePerms: 0644,
}

func (c *ModelManagerConfig) applyDefaults() {
	if c.BatchWaitTimeMin == 0 {
		c.BatchWaitTimeMin = modelManagerConfigDefaults.BatchWaitTimeMin
	}
	if c.BatchWaitTimeMax == 0 {
		c.BatchWaitTimeMax = modelManagerConfigDefaults.BatchWaitTimeMax
	}
	if c.HttpClientMaxConns == 0 {
		c.HttpClientMaxConns = modelManagerConfigDefaults.HttpClientMaxConns
	}
	if c.ReloadTimeout == 0 {
		c.ReloadTimeout = modelManagerConfigDefaults.ReloadTimeout
	}
	if c.RequestChannelSize == 0 {
		c.RequestChannelSize = modelManagerConfigDefaults.RequestChannelSize
	}
	if c.ModelConfigFilePerms == 0 {
		c.ModelConfigFilePerms = modelManagerConfigDefaults.ModelConfigFilePerms
	}
}

func NewOvmsModelManager(address string, multiModelConfigFilename string, log logr.Logger, mmConfig ModelManagerConfig) (*OvmsModelManager, error) {

	mmConfig.applyDefaults()

	// try to load the initial config from disk, if it exists
	// this handles the case where the adapter crashes
	multiModelConfig := map[string]OvmsMultiModelConfigListEntry{}
	if configBytes, err := os.ReadFile(multiModelConfigFilename); err != nil {
		// if there is any error in initialization from an existing file, just continue with an empty config
		// but log if there was an error reading an existing file
		if !errors.Is(err, os.ErrNotExist) {
			log.Error(err, "WARNING: could not initialize model config from file, will continue with empty config", "filename", multiModelConfigFilename)
		}
	} else {
		var modelRepositoryConfig OvmsMultiModelRepositoryConfig
		if err := json.Unmarshal(configBytes, &modelRepositoryConfig); err != nil {
			log.Error(err, "WARNING: could not parse model config JSON, will continue with empty config", "filename", multiModelConfigFilename)
		} else {
			multiModelConfig = make(map[string]OvmsMultiModelConfigListEntry, len(modelRepositoryConfig.ModelConfigList))
			for _, mc := range modelRepositoryConfig.ModelConfigList {
				multiModelConfig[mc.Config.Name] = mc
			}
		}
	}

	configRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/v1/config", address), http.NoBody)
	if err != nil {
		return nil, err
	}

	reloadRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/v1/config/reload", address), http.NoBody)
	if err != nil {
		return nil, err
	}

	ovmsMM := &OvmsModelManager{
		configRequest: configRequest,
		reloadRequest: reloadRequest,
		// will need to be updated before being queried
		cachedModelConfigResponse: OvmsConfigResponse{},
		config:                    mmConfig,
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        mmConfig.HttpClientMaxConns,
				MaxConnsPerHost:     mmConfig.HttpClientMaxConns,
				MaxIdleConnsPerHost: mmConfig.HttpClientMaxConns,
			},
		},
		log:                       log,
		loadedModelsMap:           multiModelConfig,
		modelConfigFilename:       multiModelConfigFilename,
		requests:                  make(chan *request, mmConfig.RequestChannelSize),
		modelRepositoryConfigList: make([]OvmsMultiModelConfigListEntry, 0, len(multiModelConfig)),
	}

	// write the config out on boot because OVMS needs it to exist
	if _, err := os.Stat(multiModelConfigFilename); os.IsNotExist(err) {
		if err = ovmsMM.writeConfig(); err != nil {
			log.Error(err, "Unable to write out empty config file")
		}
	}

	// start the actor process
	go ovmsMM.run()

	return ovmsMM, nil
}

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

	req := &request{
		requestType: load,
		modelId:     modelId,
		basePath:    basePath,
	}

	if err := mm.handleRequest(ctx, req); err != nil {
		return fmt.Errorf("LoadModel errored: %w", err)
	}
	return nil
}

func (mm *OvmsModelManager) UnloadModel(ctx context.Context, modelId string) error {
	req := &request{
		requestType: unload,
		modelId:     modelId,
	}

	if err := mm.handleRequest(ctx, req); err != nil {
		return fmt.Errorf("UnloadModel errored: %w", err)
	}
	return nil
}

func (mm *OvmsModelManager) UnloadAll(ctx context.Context) error {
	req := &request{
		requestType: unloadAll,
	}

	if err := mm.handleRequest(ctx, req); err != nil {
		return fmt.Errorf("UnloadAll errored: %w", err)
	}
	return nil
}

func (mm *OvmsModelManager) handleRequest(ctx context.Context, r *request) error {
	c := make(chan error, 1)
	r.c = c
	r.ctx = ctx

	mm.requests <- r

	select {
	case err := <-c:
		if err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("Request was cancelled")
	}
}

func (mm *OvmsModelManager) GetConfig(ctx context.Context) error {
	return mm.getConfig(ctx)
}

// internal

type requestType string

const (
	load      requestType = "Load"
	unload    requestType = "Unload"
	unloadAll requestType = "UnloadAll"
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
// Maintains a slice of batched requests that are in process in the reload
//
// Returns results from the reload operation once it completes
// Receives a stream of requests from its channel
func (mm *OvmsModelManager) run() {
	log := mm.log.WithValues("thread", "run")
	log.Info("Starting ModelManger thread")
	for mm.requests != nil {
		loadRequestsMap := mm.gatherLoadRequests()

		// gatherLoadRequests() collects requests over time, some requests
		// may have been cancelled by now. Check that here before
		// reloading the config
		for id, req := range loadRequestsMap {
			if err := req.ctx.Err(); err != nil {
				s := status.FromContextError(err)
				mm.log.V(1).Info("Aborting request with cancelled context before reloading", "model_id", id, "request_type", req.requestType, "code", s.Code())
				completeRequest(req, s.Code(), "Request context has been cancelled")

				delete(mm.loadedModelsMap, id)
				delete(loadRequestsMap, id)
			}
		}

		// reload the config
		if err := mm.updateModelConfig(); err != nil {
			msg := "Failed to update model configuration with OVMS"
			log.Error(err, msg)

			// at this point, we don't know whether OVMS has
			// reloaded or not... but we treat it as if the load
			// failed
			for id, req := range loadRequestsMap {
				completeRequest(req, codes.Internal, fmt.Sprintf("%s: %v", msg, err))
				delete(mm.loadedModelsMap, id)
			}

			continue // back to the start of the run() loop
		}

		// complete the requests
		for id, req := range loadRequestsMap {
			var statusExists bool
			var modelStatus OvmsModelVersionStatus
			modelState := "_missing_" // default value for logging purposes

			conf, statusExists := mm.cachedModelConfigResponse[id]
			if statusExists {
				modelStatus = conf.ModelVersionStatus[0]
				modelState = modelStatus.State
			}

			var code codes.Code
			var message string
			if !statusExists {
				code = codes.Internal
				message = "Expected model to load, but no status entry found in the config"
			} else if modelStatus.State == "AVAILABLE" {
				code = codes.OK
			} else {
				code = codes.Unknown
				message = fmt.Sprintf("OVMS model load failed. code: '%s' reason: '%s'", modelStatus.Status.ErrorCode, modelStatus.Status.ErrorMessage)
			}
			log.V(1).Info("Completing load request", "model_id", id, "state", modelState, "grpcCode", code, "message", message)
			completeRequest(req, code, message)

			// if the load failed, cleanup the map entry
			if code != codes.OK {
				delete(mm.loadedModelsMap, id)
			}
		}
	}

	log.Info("ModelManager thread exiting")
}

// gatherUpdates reads requests from the channel and updates the loadedModelsMap
//
// This handles deciding which requests will require a reload, completing
// requests that will not change the state and ignoring requests that are
// cancelled.
//
//
// We need a criteria to determine when to stop grabbing requests after a reload
// is needed; here we take the approach of waiting for a period of time after a
// request is received
func (mm *OvmsModelManager) gatherLoadRequests() map[string]*request {
	requestMap := map[string]*request{}
	// used to signal when to proceed with a reload after a timer
	var stopChan <-chan time.Time
	// Load calls need to trigger the model server as part of the request,
	// but an unload request can be completed immediately by removing the
	// registration from the models map (state will be synced with the next
	// updates) Though even unloads should be processed eventually. To
	// support this the timer duration for the stopChan depends on wether or
	// not a load request is included in the batch of updates, which is
	// tracked with this boolean
	shortTimerSet := false
	for {
		select {
		case <-stopChan:
			return requestMap

		case req, ok := <-mm.requests:
			if !ok {
				// shutdown the run() loop after processing this batch
				mm.requests = nil
				break // from select
			}

			// has the context been cancelled?
			if err := req.ctx.Err(); err != nil {
				s := status.FromContextError(err)
				mm.log.V(1).Info("Aborting request with cancelled context", "model_id", req.modelId, "request_type", req.requestType, "code", s.Code())
				completeRequest(req, s.Code(), "Request context has been cancelled")
				continue
			}

			switch req.requestType {
			case unloadAll:
				mm.log.V(1).Info("Processing UnloadAll", "numRequests", len(requestMap))

				// abort all pending requests
				for _, r := range requestMap {
					mm.log.V(1).Info("Aborting request to reset the Model Server", "model_id", r.modelId, "request_type", r.requestType, "context_error", r.ctx.Err().Error())
					completeRequest(r, codes.Aborted, "Model Server is being reset")
				}
				requestMap = map[string]*request{}

				// an UnloadAll does not need to trigger a reload right now, we can report success
				// and will sync state with the model server on the next reload
				completeRequest(req, codes.OK, "")

				// reset the desired model state to be empty
				mm.loadedModelsMap = map[string]OvmsMultiModelConfigListEntry{}

				// reset the stop timer
				if stopChan == nil {
					shortTimerSet = false
					stopChan = time.NewTimer(mm.config.BatchWaitTimeMax).C
				}

			case unload:
				// abort any pending load requests for this model
				if requestMap[req.modelId] != nil {
					mm.log.V(1).Info("Aborting request due to subsequent unload", "model_id", requestMap[req.modelId].modelId, "request_type", requestMap[req.modelId].requestType, "context_error", requestMap[req.modelId].ctx.Err().Error())
					completeRequest(requestMap[req.modelId], codes.Aborted, "Aborting due to subsequent unload request")
					delete(requestMap, req.modelId)
				}

				delete(mm.loadedModelsMap, req.modelId)
				// an Unload does not need to trigger a config reload, we can report
				// success and will sync state with the model server on the next reload
				completeRequest(req, codes.OK, "")

				// set the stop timer, if not already set
				if stopChan == nil {
					stopChan = time.NewTimer(mm.config.BatchWaitTimeMax).C
				}

			case load:
				// abort any pending load requests for this model
				if requestMap[req.modelId] != nil {
					mm.log.V(1).Info("Aborting request due to subsequent load", "model_id", requestMap[req.modelId].modelId, "request_type", requestMap[req.modelId].requestType, "context_error", requestMap[req.modelId].ctx.Err().Error())
					completeRequest(requestMap[req.modelId], codes.Aborted, "Aborting due to concurrent load request")
				}

				// set the stop timer, if not already set with the short timer
				if stopChan == nil || !shortTimerSet {
					shortTimerSet = true
					stopChan = time.NewTimer(mm.config.BatchWaitTimeMin).C
				}

				requestMap[req.modelId] = req
				mm.loadedModelsMap[req.modelId] = OvmsMultiModelConfigListEntry{
					Config: OvmsMultiModelModelConfig{
						Name:     req.modelId,
						BasePath: req.basePath,
					},
				}
			}
		}
	}
}

func completeRequest(req *request, code codes.Code, reason string) {
	// if code == OK, status.Error returns nil
	req.c <- status.Error(code, reason)
}

func (mm *OvmsModelManager) getConfig(ctx context.Context) error {
	// query the Config Status API
	resp, err := mm.client.Do(mm.configRequest.WithContext(ctx))
	if err != nil {
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
			const msg string = "Error parsing /config response"
			mm.log.V(1).Error(err1, msg, "responseBody", string(body))
			return fmt.Errorf("%s: %w", msg, err1)
		}
		mm.cachedModelConfigResponse = c

		return nil
	}

	var errorResponse OvmsConfigErrorResponse
	if err = json.Unmarshal(body, &errorResponse); err != nil {
		const msg string = "Error parsing /config error response"
		mm.log.V(1).Error(err, msg, "responseBody", string(body))
		return fmt.Errorf("%s: %w", msg, err)
	}

	errDesc := fmt.Errorf("Error response when getting the config: %s", errorResponse.Error)
	mm.log.Error(errDesc, "Call to /v1/config returned an error", "code", resp.StatusCode)

	return status.Error(codes.Internal, errDesc.Error())
}

func (mm *OvmsModelManager) writeConfig() error {
	// reset the stored list and ensure sufficient capacity
	if cap(mm.modelRepositoryConfigList) < len(mm.loadedModelsMap) {
		mm.modelRepositoryConfigList = make([]OvmsMultiModelConfigListEntry, len(mm.loadedModelsMap))
	} else {
		mm.modelRepositoryConfigList = mm.modelRepositoryConfigList[:len(mm.loadedModelsMap)]
	}

	listIndex := 0
	for _, model := range mm.loadedModelsMap {
		mm.modelRepositoryConfigList[listIndex] = model
		listIndex++
	}

	modelRepositoryConfigJSON, err := json.Marshal(OvmsMultiModelRepositoryConfig{mm.modelRepositoryConfigList})
	if err != nil {
		return fmt.Errorf("Error marshalling config file: %w", err)
	}

	if err := ioutil.WriteFile(mm.modelConfigFilename, modelRepositoryConfigJSON, mm.config.ModelConfigFilePerms); err != nil {
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
func (mm *OvmsModelManager) updateModelConfig() error {
	ctx, cancel := context.WithTimeout(context.Background(), mm.config.ReloadTimeout)
	defer cancel()

	if err := mm.writeConfig(); err != nil {
		return fmt.Errorf("Error updating model config when writing config file: %w", err)
	}

	// Send config reload request to OVMS
	//
	// Handling of the response
	// - If connection error: return the error
	// - If timeout: Maybe should have retries?
	// - If response is 201 or 200: parse and cache the model config
	// - If other HTTP error, check error message in JSON
	//    If model load error: query the config status API
	//    If other error, just return it?
	resp, err := mm.client.Do(mm.reloadRequest.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("Communication error reloading the config: %w", err)
	}
	defer resp.Body.Close()

	// Read the body
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
			const msg string = "Error parsing /config/reload response"
			mm.log.V(1).Error(err, msg, "responseBody", string(body))
			return fmt.Errorf("%s: %w", msg, err)
		}
		mm.cachedModelConfigResponse = c

		return nil
	}

	// Config reload failed, try to figure out why
	// The response will not include the model statuses, but we can query
	// for the config separately to get details on the failing models
	var errorResponse OvmsConfigErrorResponse
	if err = json.Unmarshal(body, &errorResponse); err != nil {
		const msg string = "Error parsing /config/reload error response"
		mm.log.V(1).Error(err, msg, "responseBody", string(body))
		return fmt.Errorf("%s: %w", msg, err)
	}

	mm.log.Error(fmt.Errorf("Error response when reloading the config: %s", errorResponse.Error), "Call to /v1/config/reload returned an error", "code", resp.StatusCode)

	// we rely on the fact that getConfig updates cachedModelConfigResponse
	return mm.getConfig(ctx)
}
