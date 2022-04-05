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
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
	"github.com/kserve/modelmesh-runtime-adapter/model-serving-puller/puller"
)

type AdapterConfiguration struct {
	Port                     int
	OvmsPort                 int
	OvmsContainerMemReqBytes int
	OvmsMemBufferBytes       int
	CapacityInBytes          int
	MaxLoadingConcurrency    int
	ModelLoadingTimeoutMS    int
	DefaultModelSizeInBytes  int
	ModelSizeMultiplier      float64
	RuntimeVersion           string
	LimitModelConcurrency    int // 0 means no limit (default)
	RootModelDir             string
	UseEmbeddedPuller        bool

	// OVMS adapter specific
	ModelConfigFile  string
	BatchWaitTimeMin time.Duration
	BatchWaitTimeMax time.Duration
	ReloadTimeout    time.Duration
}

type OvmsAdapterServer struct {
	ModelManager  *OvmsModelManager
	Puller        *puller.Puller
	AdapterConfig *AdapterConfiguration
	Log           logr.Logger

	// embed generated Unimplemented type for forward-compatibility for gRPC
	mmesh.UnimplementedModelRuntimeServer
}

func NewOvmsAdapterServer(runtimePort int, config *AdapterConfiguration, log logr.Logger) *OvmsAdapterServer {
	log = log.WithName("Openvino Model Server Adapter Server")
	log.Info("Connecting to Openvino Model Server...", "port", runtimePort)

	s := new(OvmsAdapterServer)
	s.Log = log
	s.AdapterConfig = config

	if mm, err := NewOvmsModelManager(
		fmt.Sprintf("http://localhost:%d", config.OvmsPort),
		config.ModelConfigFile,
		log,
		ModelManagerConfig{
			BatchWaitTimeMin: config.BatchWaitTimeMin,
			BatchWaitTimeMax: config.BatchWaitTimeMax,
			ReloadTimeout:    config.ReloadTimeout,
		},
	); err != nil {
		panic(err)
	} else {
		s.ModelManager = mm
	}

	if s.AdapterConfig.UseEmbeddedPuller {
		// puller is configured from its own env vars
		s.Puller = puller.NewPuller(log)
	}

	// send simple request to verify the connection, with retries
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		err := s.ModelManager.GetConfig(ctx)
		if err == nil {
			break
		}
		log.Info("Adapter failed to ping OVMS, will retry", "error", err.Error())
		time.Sleep(1 * time.Second)
	}

	// if the context is cancelled, we could not connect
	if ctx.Err() != nil {
		log.Error(ctx.Err(), "Adapter failed to connect to OVMS")
		os.Exit(1)
	}
	log.Info("OVMS Runtime connected!")

	return s
}

func (s *OvmsAdapterServer) LoadModel(ctx context.Context, req *mmesh.LoadModelRequest) (*mmesh.LoadModelResponse, error) {
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
		log.Error(loadErr, "OVMS failed to load model")
		return nil, status.Errorf(status.Code(loadErr), "Failed to load model due to error: %s", loadErr)
	}

	size := util.CalcMemCapacity(req.ModelKey, s.AdapterConfig.DefaultModelSizeInBytes, s.AdapterConfig.ModelSizeMultiplier, log)

	log.Info("OVMS model loaded", "sizeInBytes", size)

	return &mmesh.LoadModelResponse{
		SizeInBytes:    size,
		MaxConcurrency: uint32(s.AdapterConfig.LimitModelConcurrency),
	}, nil
}

func (s *OvmsAdapterServer) UnloadModel(ctx context.Context, req *mmesh.UnloadModelRequest) (*mmesh.UnloadModelResponse, error) {
	unloadErr := s.ModelManager.UnloadModel(ctx, req.ModelId)
	if unloadErr != nil {
		// check if we got a gRPC error as a response that indicates that OVMS
		// does not have the model registered. In that case we still want to proceed
		// with removing the model files.
		if grpcStatus, ok := status.FromError(unloadErr); ok && grpcStatus.Code() == codes.NotFound {
			s.Log.Info("Unload request for model not found in OVMS", "error", unloadErr, "model_id", req.ModelId)
		} else {
			s.Log.Error(unloadErr, "Failed to unload model from OVMS", "model_id", req.ModelId)
			return nil, status.Errorf(status.Code(unloadErr), "Failed to unload model from OVMS")
		}
	}

	ovmsModelIDDir, err := util.SecureJoin(s.AdapterConfig.RootModelDir, ovmsModelSubdir, req.ModelId)
	if err != nil {
		s.Log.Error(err, "Unable to securely join", "rootModelDir", s.AdapterConfig.RootModelDir, "ovmsModelSubdir", ovmsModelSubdir, "modelId", req.ModelId)
		return nil, err
	}
	err = os.RemoveAll(ovmsModelIDDir)
	if err != nil {
		return nil, status.Errorf(status.Code(err), "Error while deleting the %s dir: %v", ovmsModelIDDir, err)
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
func (s *OvmsAdapterServer) PredictModelSize(ctx context.Context, req *mmesh.PredictModelSizeRequest) (*mmesh.PredictModelSizeResponse, error) {
	size := s.AdapterConfig.DefaultModelSizeInBytes
	return &mmesh.PredictModelSizeResponse{SizeInBytes: uint64(size)}, nil
}

func (s *OvmsAdapterServer) ModelSize(ctx context.Context, req *mmesh.ModelSizeRequest) (*mmesh.ModelSizeResponse, error) {
	size := s.AdapterConfig.DefaultModelSizeInBytes // TODO find out size

	return &mmesh.ModelSizeResponse{SizeInBytes: uint64(size)}, nil
}

func (s *OvmsAdapterServer) RuntimeStatus(ctx context.Context, req *mmesh.RuntimeStatusRequest) (*mmesh.RuntimeStatusResponse, error) {
	log := s.Log
	runtimeStatus := new(mmesh.RuntimeStatusResponse)

	ovmsErr := s.ModelManager.GetConfig(ctx)
	if ovmsErr != nil {
		log.Info("Failed to ping OVMS", "error", ovmsErr)
		runtimeStatus.Status = mmesh.RuntimeStatusResponse_STARTING
		return runtimeStatus, nil
	}

	// Reset OVMS, unloading any existing models
	unloadErr := s.ModelManager.UnloadAll(ctx)
	if unloadErr != nil {
		runtimeStatus.Status = mmesh.RuntimeStatusResponse_STARTING
		log.Info("Unloading all OVMS models failed", "error", unloadErr)
		return runtimeStatus, nil
	}

	runtimeStatus.Status = mmesh.RuntimeStatusResponse_READY
	runtimeStatus.CapacityInBytes = uint64(s.AdapterConfig.CapacityInBytes)
	runtimeStatus.MaxLoadingConcurrency = uint32(s.AdapterConfig.MaxLoadingConcurrency)
	runtimeStatus.ModelLoadingTimeoutMs = uint32(s.AdapterConfig.ModelLoadingTimeoutMS)
	runtimeStatus.DefaultModelSizeInBytes = uint64(s.AdapterConfig.DefaultModelSizeInBytes)
	runtimeStatus.RuntimeVersion = s.AdapterConfig.RuntimeVersion
	runtimeStatus.LimitModelConcurrency = s.AdapterConfig.LimitModelConcurrency > 0

	// OVMS only supports the Predict API currently
	path_1_1 := []uint32{1, 1} // PredictRequest[model_spec][name]
	mis := make(map[string]*mmesh.RuntimeStatusResponse_MethodInfo)
	mis["tensorflow.serving.PredictionService/Predict"] = &mmesh.RuntimeStatusResponse_MethodInfo{IdInjectionPath: path_1_1}
	runtimeStatus.MethodInfos = mis

	log.Info("runtimeStatus", "Status", runtimeStatus)
	return runtimeStatus, nil
}
