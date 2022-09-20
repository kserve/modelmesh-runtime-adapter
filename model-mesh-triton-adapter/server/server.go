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
	"os"
	"time"

	"google.golang.org/grpc/credentials/insecure"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	triton "github.com/kserve/modelmesh-runtime-adapter/internal/proto/triton"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
	"github.com/kserve/modelmesh-runtime-adapter/model-serving-puller/puller"
)

type AdapterConfiguration struct {
	Port                       int
	TritonPort                 int
	TritonContainerMemReqBytes int
	TritonMemBufferBytes       int
	CapacityInBytes            int
	MaxLoadingConcurrency      int
	ModelLoadingTimeoutMS      int
	DefaultModelSizeInBytes    int
	ModelSizeMultiplier        float64
	RuntimeVersion             string
	LimitModelConcurrency      int // 0 means no limit (default)
	RootModelDir               string
	UseEmbeddedPuller          bool
}

type TritonAdapterServer struct {
	Client        triton.GRPCInferenceServiceClient
	Conn          *grpc.ClientConn
	Puller        *puller.Puller
	AdapterConfig *AdapterConfiguration
	Log           logr.Logger

	// embed generated Unimplemented type for forward-compatibility for gRPC
	mmesh.UnimplementedModelRuntimeServer
}

func NewTritonAdapterServer(runtimePort int, config *AdapterConfiguration, log logr.Logger) *TritonAdapterServer {
	log = log.WithName("Triton Adapter Server")

	log.Info("Connecting to Triton...", "port", runtimePort)
	conn, err := grpc.DialContext(context.Background(), fmt.Sprintf("localhost:%d", runtimePort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(grpc.ConnectParams{Backoff: backoff.DefaultConfig, MinConnectTimeout: 10 * time.Second}))
	if err != nil {
		log.Error(err, "Can not connect to Triton Runtime")
		os.Exit(1)
	}

	s := new(TritonAdapterServer)
	s.Log = log
	s.AdapterConfig = config
	s.Client = triton.NewGRPCInferenceServiceClient(conn)
	s.Conn = conn
	if s.AdapterConfig.UseEmbeddedPuller {
		// puller is configured from its own env vars
		s.Puller = puller.NewPuller(log)
	}

	log.Info("Triton runtime adapter started")
	return s
}

func (s *TritonAdapterServer) LoadModel(ctx context.Context, req *mmesh.LoadModelRequest) (*mmesh.LoadModelResponse, error) {
	log := s.Log.WithName("Load Model").WithValues("model_id", req.ModelId)
	modelType := getModelType(req, log)
	log.Info("Using model type", "model_type", modelType)

	if s.AdapterConfig.UseEmbeddedPuller {
		var pullerErr error
		req, pullerErr = s.Puller.ProcessLoadModelRequest(ctx, req)
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

	_, tritonErr := s.Client.RepositoryModelLoad(ctx, &triton.RepositoryModelLoadRequest{
		ModelName: req.ModelId,
	})
	if tritonErr != nil {
		log.Error(tritonErr, "Triton failed to load model")
		return nil, status.Errorf(status.Code(tritonErr), "Failed to load Model due to Triton runtime error: %s", tritonErr)
	}

	size := util.CalcMemCapacity(req.ModelKey, s.AdapterConfig.DefaultModelSizeInBytes, s.AdapterConfig.ModelSizeMultiplier, log)

	log.Info("Triton model loaded")

	return &mmesh.LoadModelResponse{
		SizeInBytes:    size,
		MaxConcurrency: uint32(s.AdapterConfig.LimitModelConcurrency),
	}, nil
}

// getModelType first tries to read the type from the LoadModelRequest.ModelKey json
// If there is an error parsing LoadModelRequest.ModelKey or the type is not found there, this will
// return the LoadModelRequest.ModelType which could possibly be an empty string
func getModelType(req *mmesh.LoadModelRequest, log logr.Logger) string {
	modelType := req.ModelType
	var modelKey map[string]interface{}
	err := json.Unmarshal([]byte(req.ModelKey), &modelKey)
	if err != nil {
		log.Info("The model type will fall back to LoadModelRequest.ModelType as LoadModelRequest.ModelKey value is not valid JSON", "model_type", req.ModelType, "model_key", req.ModelKey, "error", err)
	} else if modelKey[modelTypeJSONKey] == nil {
		log.Info("The model type will fall back to LoadModelRequest.ModelType as LoadModelRequest.ModelKey does not have specified attribute", "model_type", req.ModelType, "attribute", modelTypeJSONKey)
	} else if modelKeyModelType, ok := modelKey[modelTypeJSONKey].(map[string]interface{}); ok {
		if str, ok := modelKeyModelType["name"].(string); ok {
			modelType = str
		} else {
			log.Info("The model type will fall back to LoadModelRequest.ModelType as LoadModelRequest.ModelKey attribute is not a string.", "model_type", req.ModelType, "attribute", modelTypeJSONKey, "attribute_value", modelKey[modelTypeJSONKey])
		}
	} else if str, ok := modelKey[modelTypeJSONKey].(string); ok {
		modelType = str
	} else {
		log.Info("The model type will fall back to LoadModelRequest.ModelType as LoadModelRequest.ModelKey attribute is not a string or map[string].", "model_type", req.ModelType, "attribute", modelTypeJSONKey, "attribute_value", modelKey[modelTypeJSONKey])
	}
	return modelType
}

func (s *TritonAdapterServer) UnloadModel(ctx context.Context, req *mmesh.UnloadModelRequest) (*mmesh.UnloadModelResponse, error) {
	_, tritonErr := s.Client.RepositoryModelUnload(ctx, &triton.RepositoryModelUnloadRequest{
		ModelName: req.ModelId,
	})

	if tritonErr != nil {
		// check if we got a gRPC error as a response that indicates that Triton
		// does not have the model registered. In that case we still want to proceed
		// with removing the model files.
		// Currently, Triton returns OK if a model does not exist, but handle
		// NOT_FOUND in case the response changes in the future.
		if grpcStatus, ok := status.FromError(tritonErr); ok && grpcStatus.Code() == codes.NotFound {
			s.Log.Info("Unload request for model not found in Triton", "error", tritonErr, "model_id", req.ModelId)
		} else {
			s.Log.Error(tritonErr, "Failed to unload model from Triton", "model_id", req.ModelId)
			return nil, status.Errorf(status.Code(tritonErr), "Failed to unload model from Triton")
		}
	}

	tritonModelIDDir, err := util.SecureJoin(s.AdapterConfig.RootModelDir, req.ModelId)
	if err != nil {
		s.Log.Error(err, "Unable to securely join", "rootModelDir", s.AdapterConfig.RootModelDir, "modelId", req.ModelId)
		return nil, err
	}
	err = os.RemoveAll(tritonModelIDDir)
	if err != nil {
		return nil, status.Errorf(status.Code(err), "Error while deleting the %s dir: %v", tritonModelIDDir, err)
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

func (s *TritonAdapterServer) RuntimeStatus(ctx context.Context, req *mmesh.RuntimeStatusRequest) (*mmesh.RuntimeStatusResponse, error) {
	log := s.Log
	runtimeStatus := &mmesh.RuntimeStatusResponse{Status: mmesh.RuntimeStatusResponse_STARTING}

	serverReadyResponse, tritonErr := s.Client.ServerReady(ctx, &triton.ServerReadyRequest{})

	if tritonErr != nil {
		log.Info("Triton failed to get status or not ready", "error", tritonErr)
		return runtimeStatus, nil
	}

	if !serverReadyResponse.Ready {
		log.Info("Triton runtime not ready")
		return runtimeStatus, nil
	}

	// unloading if there are any models already loaded
	indexResponse, tritonErr := s.Client.RepositoryIndex(ctx, &triton.RepositoryIndexRequest{
		RepositoryName: "",
		Ready:          true})

	if tritonErr != nil {
		log.Info("Triton runtime status, getting model info failed", "error", tritonErr)
		return runtimeStatus, nil
	}

	for model := range indexResponse.Models {
		if _, tritonErr := s.Client.RepositoryModelUnload(ctx, &triton.RepositoryModelUnloadRequest{
			ModelName: indexResponse.Models[model].Name,
		}); tritonErr != nil {
			s.Log.Info("Triton runtime status, unload model failed", "error", tritonErr)
			return runtimeStatus, nil
		}
	}

	// Clear adapted model dirs
	if err := util.ClearDirectoryContents(s.AdapterConfig.RootModelDir, nil); err != nil {
		log.Error(err, "Error cleaning up local model dir")
		return &mmesh.RuntimeStatusResponse{Status: mmesh.RuntimeStatusResponse_FAILING}, nil
	}

	if s.AdapterConfig.UseEmbeddedPuller {
		if err := s.Puller.ClearLocalModelStorage(tritonModelSubdir); err != nil {
			log.Error(err, "Error cleaning up local model dir")
			return &mmesh.RuntimeStatusResponse{Status: mmesh.RuntimeStatusResponse_FAILING}, nil
		}
	}

	resp, err := s.Client.ServerMetadata(ctx, &triton.ServerMetadataRequest{})
	if err != nil {
		log.Info("Warning: Triton failed to get version from server metadata", "error", err)
	}
	if resp.Version != "" {
		log.Info("Using runtime version returned by Triton", "version", resp.Version)
		s.AdapterConfig.RuntimeVersion = resp.Version
	}

	runtimeStatus.Status = mmesh.RuntimeStatusResponse_READY
	runtimeStatus.CapacityInBytes = uint64(s.AdapterConfig.CapacityInBytes)
	runtimeStatus.MaxLoadingConcurrency = uint32(s.AdapterConfig.MaxLoadingConcurrency)
	runtimeStatus.ModelLoadingTimeoutMs = uint32(s.AdapterConfig.ModelLoadingTimeoutMS)
	runtimeStatus.DefaultModelSizeInBytes = uint64(s.AdapterConfig.DefaultModelSizeInBytes)
	runtimeStatus.RuntimeVersion = s.AdapterConfig.RuntimeVersion
	runtimeStatus.LimitModelConcurrency = s.AdapterConfig.LimitModelConcurrency > 0

	path1 := []uint32{1}

	mis := make(map[string]*mmesh.RuntimeStatusResponse_MethodInfo)
	mis[tritonServiceName+"/ModelInfer"] = &mmesh.RuntimeStatusResponse_MethodInfo{IdInjectionPath: path1}
	mis[tritonServiceName+"/ModelMetadata"] = &mmesh.RuntimeStatusResponse_MethodInfo{IdInjectionPath: path1}
	runtimeStatus.MethodInfos = mis

	log.Info("runtimeStatus", "Status", runtimeStatus)
	return runtimeStatus, nil
}
