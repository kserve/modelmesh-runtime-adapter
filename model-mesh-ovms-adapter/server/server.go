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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
	"github.com/kserve/modelmesh-runtime-adapter/model-serving-puller/puller"
)

type AdapterConfiguration struct {
	Port                         int
	OpenVinoPort                 int
	OpenvinoContainerMemReqBytes int
	OpenvinoMemBufferBytes       int
	CapacityInBytes              int
	MaxLoadingConcurrency        int
	ModelLoadingTimeoutMS        int
	DefaultModelSizeInBytes      int
	ModelSizeMultiplier          float64
	RuntimeVersion               string
	LimitModelConcurrency        int // 0 means no limit (default)
	RootModelDir                 string
	ModelConfigFile              string
	UseEmbeddedPuller            bool
}

type OpenvinoAdapterServer struct {
	ModelManager  *OvmsModelManager
	Puller        *puller.Puller
	AdapterConfig *AdapterConfiguration
	Log           logr.Logger
}

type OvmsModelManager struct {
	address             string
	modelConfigFilename string

	loadedModelsMap map[string]OvmsMultiModelConfigListEntry
	client          *http.Client
	mux             sync.Mutex
}

func NewOvmsModelManager(address string, configFilename string) *OvmsModelManager {

	ovmsMM := &OvmsModelManager{
		address:             address,
		modelConfigFilename: configFilename,
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

	// write the config out on boot since OVMS needs it to exist

	return ovmsMM
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

// reloadConfig triggers OVMS to reload
// An error is returned if:
// - the config fails the schema check
// - if ANY of the configured models fail to load
func (mm *OvmsModelManager) reloadConfig(ctx context.Context) error {
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

func (mm *OvmsModelManager) UnoadModel(ctx context.Context, modelId string) error {
	mm.mux.Lock()
	defer mm.mux.Unlock()

	delete(mm.loadedModelsMap, modelId)

	if err := mm.reloadConfig(ctx); err != nil {
		return err
	}

	return nil
}

func NewOpenVinoAdapterServer(runtimePort int, config *AdapterConfiguration, log logr.Logger) *OpenvinoAdapterServer {
	log = log.WithName("Openvino Adapter Server")
	log.Info("Connecting to Openvino...", "port", runtimePort)

	s := new(OpenvinoAdapterServer)
	s.Log = log
	s.AdapterConfig = config
	s.ModelManager = NewOvmsModelManager(fmt.Sprintf("http://localhost:%d", config.OpenVinoPort), config.ModelConfigFile)

	if s.AdapterConfig.UseEmbeddedPuller {
		// puller is configured from its own env vars
		s.Puller = puller.NewPuller(log)
	}

	// TODO: send simple request to test connection at boot
	// log.Info("Openvino Runtime connected!")

	return s
}

func (s *OpenvinoAdapterServer) LoadModel(ctx context.Context, req *mmesh.LoadModelRequest) (*mmesh.LoadModelResponse, error) {
	log := s.Log.WithName("Load Model").WithValues("model_id", req.ModelId)
	modelType := util.GetModelType(req, log)
	log.Info("Using model type", "model_type", modelType)

	if s.AdapterConfig.UseEmbeddedPuller {
		var pullerErr error
		req, pullerErr = s.Puller.ProcessLoadModelRequest(req)
		if pullerErr != nil {
			log.Error(pullerErr, "Failed to pull model from storage")
			return nil, pullerErr
		}
	}

	var err error
	schemaPath, err := util.GetSchemaPath(req)
	if err != nil {
		return nil, err
	}

	// using the files downloaded by the puller, create a file layout that the runtime can understand and load from
	err = adaptModelLayoutForRuntime(ctx, s.AdapterConfig.RootModelDir, req.ModelId, modelType, req.ModelPath, schemaPath, log)
	if err != nil {
		log.Error(err, "Failed to create model directory and load model")
		return nil, status.Errorf(status.Code(err), "Failed to load Model due to adapter error: %s", err)
	}

	adaptedModelPath, err := util.SecureJoin(s.AdapterConfig.RootModelDir, ovmsModelSubdir, req.ModelId)
	if err != nil {
		log.Error(err, "Unable to securely join", "rootModelDir", rootModelDir, "ovmsModelSubdir", ovmsModelSubdir, "modelID", req.ModelId)
		return nil, err
	}

	loadErr := s.ModelManager.LoadModel(ctx, adaptedModelPath, req.ModelId)
	if loadErr != nil {
		log.Error(loadErr, "Openvino failed to load model")
		return nil, status.Errorf(status.Code(loadErr), "Failed to load Model due to Openvino runtime error: %s", loadErr)
	}

	size := util.CalcMemCapacity(req.ModelKey, s.AdapterConfig.DefaultModelSizeInBytes, s.AdapterConfig.ModelSizeMultiplier, log)

	log.Info("Openvino model loaded")

	return &mmesh.LoadModelResponse{
		SizeInBytes:    size,
		MaxConcurrency: uint32(s.AdapterConfig.LimitModelConcurrency),
	}, nil
}

func (s *OpenvinoAdapterServer) UnloadModel(ctx context.Context, req *mmesh.UnloadModelRequest) (*mmesh.UnloadModelResponse, error) {
	unloadErr := s.ModelManager.UnoadModel(ctx, req.ModelId)
	if unloadErr != nil {
		// check if we got a gRPC error as a response that indicates that Openvino
		// does not have the model registered. In that case we still want to proceed
		// with removing the model files.
		if grpcStatus, ok := status.FromError(unloadErr); ok && grpcStatus.Code() == codes.NotFound {
			s.Log.Info("Unload request for model not found in Openvino", "error", unloadErr, "model_id", req.ModelId)
		} else {
			s.Log.Error(unloadErr, "Failed to unload model from Openvino", "model_id", req.ModelId)
			return nil, status.Errorf(status.Code(unloadErr), "Failed to unload model from Openvino")
		}
	}

	openvinoModelIDDir, err := util.SecureJoin(s.AdapterConfig.RootModelDir, openvinoModelSubdir, req.ModelId)
	if err != nil {
		s.Log.Error(err, "Unable to securely join", "rootModelDir", s.AdapterConfig.RootModelDir, "openvinoModelSubdir", openvinoModelSubdir, "modelId", req.ModelId)
		return nil, err
	}
	err = os.RemoveAll(openvinoModelIDDir)
	if err != nil {
		return nil, status.Errorf(status.Code(err), "Error while deleting the %s dir: %v", openvinoModelIDDir, err)
	}

	if s.AdapterConfig.UseEmbeddedPuller {
		// delete files from puller cache
		err = s.Puller.CleanupModel(req.ModelId)
		if err != nil {
			return nil, status.Errorf(status.Code(err), "Failed to delete model files from puller cache: %s", err)
		}
	}

	return &mmesh.UnloadModelResponse{}, nil
}

//TODO: this implementation need to be reworked
func (s *OpenvinoAdapterServer) PredictModelSize(ctx context.Context, req *mmesh.PredictModelSizeRequest) (*mmesh.PredictModelSizeResponse, error) {
	size := s.AdapterConfig.DefaultModelSizeInBytes
	return &mmesh.PredictModelSizeResponse{SizeInBytes: uint64(size)}, nil
}

func (s *OpenvinoAdapterServer) ModelSize(ctx context.Context, req *mmesh.ModelSizeRequest) (*mmesh.ModelSizeResponse, error) {
	size := s.AdapterConfig.DefaultModelSizeInBytes // TODO find out size

	return &mmesh.ModelSizeResponse{SizeInBytes: uint64(size)}, nil
}

func (s *OpenvinoAdapterServer) RuntimeStatus(ctx context.Context, req *mmesh.RuntimeStatusRequest) (*mmesh.RuntimeStatusResponse, error) {
	log := s.Log
	runtimeStatus := new(mmesh.RuntimeStatusResponse)

	_, openvinoErr := s.ModelManager.GetConfig(ctx)
	if openvinoErr != nil {
		log.Info("Failed to ping OVMS", "error", openvinoErr)
		runtimeStatus.Status = mmesh.RuntimeStatusResponse_STARTING
		return runtimeStatus, nil
	}

	// Reset OVMS, unloading any existing models
	unloadErr := s.ModelManager.UnloadAll()
	if unloadErr != nil {
		runtimeStatus.Status = mmesh.RuntimeStatusResponse_STARTING
		log.Info("Unloading all OpenVINO models failed", "error", unloadErr)
		return runtimeStatus, nil
	}

	runtimeStatus.Status = mmesh.RuntimeStatusResponse_READY
	runtimeStatus.CapacityInBytes = uint64(s.AdapterConfig.CapacityInBytes)
	runtimeStatus.MaxLoadingConcurrency = uint32(s.AdapterConfig.MaxLoadingConcurrency)
	runtimeStatus.ModelLoadingTimeoutMs = uint32(s.AdapterConfig.ModelLoadingTimeoutMS)
	runtimeStatus.DefaultModelSizeInBytes = uint64(s.AdapterConfig.DefaultModelSizeInBytes)
	runtimeStatus.RuntimeVersion = s.AdapterConfig.RuntimeVersion
	runtimeStatus.LimitModelConcurrency = s.AdapterConfig.LimitModelConcurrency > 0

	// OpenVINO only supports the Predict API currently
	path_1_1 := []uint32{1, 1} // PredictRequest[model_spec][name]
	mis := make(map[string]*mmesh.RuntimeStatusResponse_MethodInfo)
	mis["tensorflow.serving.PredictionService/Predict"] = &mmesh.RuntimeStatusResponse_MethodInfo{IdInjectionPath: path_1_1}
	runtimeStatus.MethodInfos = mis

	log.Info("runtimeStatus", "Status", runtimeStatus)
	return runtimeStatus, nil
}
