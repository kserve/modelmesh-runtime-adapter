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
	"path/filepath"
	"regexp"
	"strings"

	"google.golang.org/grpc/connectivity"

	"os"
	"time"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/torchserve"
	"google.golang.org/protobuf/types/known/emptypb"

	"google.golang.org/grpc/credentials/insecure"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
	"github.com/kserve/modelmesh-runtime-adapter/model-serving-puller/puller"
)

const (
	KServeServiceName     string = "inference.GRPCInferenceService"
	torchServeServiceName string = "org.pytorch.serve.grpc.inference.InferenceAPIsService"
	configFileName        string = "mmconfig.properties"
)

type AdapterConfiguration struct {
	Port                           int
	TorchServeManagementPort       int
	TorchServeInferenceEndpoint    string
	TorchServeContainerMemReqBytes int
	TorchServeMemBufferBytes       int
	CapacityInBytes                int
	MaxLoadingConcurrency          int
	ModelLoadingTimeoutMS          int
	DefaultModelSizeInBytes        int
	ModelSizeMultiplier            float64
	RuntimeVersion                 string
	LimitModelConcurrency          int // 0 means no limit (default)
	ModelStoreDir                  string
	UseEmbeddedPuller              bool
	RequestBatchSize               int32
	MaxBatchDelaySecs              int32
}

type TorchServeAdapterServer struct {
	ManagementClient  torchserve.ManagementAPIsServiceClient
	ManagementConn    *grpc.ClientConn
	Puller            *puller.Puller
	AdapterConfig     *AdapterConfiguration
	Log               logr.Logger
	InferenceEndpoint string
	InferenceClient   torchserve.InferenceAPIsServiceClient
	InferenceConn     *grpc.ClientConn

	// embed generated Unimplemented type for forward-compatibility for gRPC
	mmesh.UnimplementedModelRuntimeServer
}

func NewTorchServeAdapterServer(config *AdapterConfiguration, log logr.Logger) *TorchServeAdapterServer {
	log = log.WithName("TorchServe Adapter Server")

	if err := setupModelStoreDirAndConfigFile(config, log); err != nil {
		log.Error(err, "Error setting up TorchServe model store directory and/or config file")
		os.Exit(1)
	}

	runtimePort := config.TorchServeManagementPort
	runtimeInferenceEndpoint, err := util.ResolveLocalGrpcEndpoint(config.TorchServeInferenceEndpoint)
	if err != nil {
		log.Error(err, "Failed to parse TorchServe inferencing endpoint",
			"endpoint", config.TorchServeInferenceEndpoint)
		os.Exit(1)
	}

	log.Info("Connecting to TorchServe...", "port", runtimePort, "inferenceEndpoint", runtimeInferenceEndpoint)
	mconn, err := grpc.DialContext(context.Background(), fmt.Sprintf("localhost:%d", runtimePort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(grpc.ConnectParams{Backoff: backoff.DefaultConfig, MinConnectTimeout: 10 * time.Second}))
	if err != nil {
		log.Error(err, "Can not connect to TorchServe server")
		os.Exit(1)
	}

	s := &TorchServeAdapterServer{
		ManagementClient:  torchserve.NewManagementAPIsServiceClient(mconn),
		ManagementConn:    mconn,
		AdapterConfig:     config,
		Log:               log,
		InferenceEndpoint: runtimeInferenceEndpoint,
	}
	if s.AdapterConfig.UseEmbeddedPuller {
		// puller is configured from its own env vars
		s.Puller = puller.NewPuller(log)
	}

	log.Info("TorchServe runtime adapter started")
	return s
}

func setupModelStoreDirAndConfigFile(config *AdapterConfiguration, log logr.Logger) error {
	modelStoreDir := config.ModelStoreDir
	if err := os.MkdirAll(modelStoreDir, 0755); err != nil {
		return fmt.Errorf("failed to create model store directory %s: %w", modelStoreDir, err)
	}
	log.Info("Created TorchServe model store directory", "path", modelStoreDir)

	configFilePath := filepath.Join(modelStoreDir, configFileName)
	configFileTempPath := configFilePath + ".tmp"

	configFileContents := fmt.Sprintf(`
enable_envvars_config=true
model_store=%s
metric_time_interval=10
# The REST inference_address is set to use a different port
# than its default 8080 which clashes with that used for modelmesh internal litelinks communication
inference_address=http://127.0.0.1:8083
# Recommended performance defaults - TBC, can be overriden by TC_ env vars
number_of_netty_threads=8
job_queue_size=512
`, modelStoreDir)

	if err := os.WriteFile(configFileTempPath, []byte(configFileContents), 0664); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", configFileTempPath, err)
	}
	// Move so that creation is atomic (torchserve container will poll for existence before starting)
	if err := os.Rename(configFileTempPath, configFilePath); err != nil {
		return fmt.Errorf("failed to rename config file %s to %s: %w", configFileTempPath, configFilePath, err)
	}
	log.Info("Created TorchServe config file", "path", configFilePath)
	return nil
}

func (s *TorchServeAdapterServer) LoadModel(ctx context.Context, req *mmesh.LoadModelRequest) (*mmesh.LoadModelResponse, error) {
	log := s.Log.WithName("LoadModel").WithValues("modelId", req.ModelId)
	modelType := util.GetModelType(req, log)
	log.Info("Using model type", "modelType", modelType)

	if s.AdapterConfig.UseEmbeddedPuller {
		var pullerErr error
		req, pullerErr = s.Puller.ProcessLoadModelRequest(ctx, req)
		if pullerErr != nil {
			log.Error(pullerErr, "Failed to pull model from storage")
			return nil, pullerErr
		}
	}

	// We won't use schema for torchserve
	schemaPath, _ := util.GetSchemaPath(req)
	if schemaPath != "" {
		log.Info("Warning: Ignoring provided schema, not supported with torchserve",
			"schemaPath", schemaPath)
	}

	marFileName, err := linkModelFile(req.ModelPath, req.ModelId, s.AdapterConfig.ModelStoreDir, log)
	if err != nil {
		return nil, err
	}

	//TODO tbd about configuring larger number in non-concurrency constraint case; check how scaling works
	workerCount := int32(s.AdapterConfig.LimitModelConcurrency)
	if workerCount <= 0 {
		workerCount = 1
	}

	regResp, err := s.ManagementClient.RegisterModel(ctx, &torchserve.RegisterModelRequest{
		ModelName:      req.ModelId,
		Url:            marFileName,
		Synchronous:    true,
		InitialWorkers: workerCount,

		//TODO if/when we support arbitrary predictor-level parameters, we could extract some here
		// from the req.ModelKey json, such as a custom handler name, batch size, worker count etc.
		BatchSize:     s.AdapterConfig.RequestBatchSize,
		MaxBatchDelay: s.AdapterConfig.MaxBatchDelaySecs,

		// This is per-inference request timeout. We shouldn't need to set it because
		// the standard gRPC context timeout should be used, need to check if torchserve honors that.
		//ResponseTimeout: timeoutSecs,
	})
	if err != nil {
		log.Error(err, "TorchServe RegisterModel call failed")
		return nil, status.Errorf(status.Code(err), "Failed to load model due to TorchServe register error: %v", err)
	}

	// message here looks like "Model \"mymodel\" Version: 1.0 registered with 1 initial workers"
	log.Info("TorchServe model registration complete", "message", regResp.GetMsg())

	return &mmesh.LoadModelResponse{
		// Note we don't include SizeInBytes field here, which will cause model-mesh to immediately call ModelSize rpc (below).
		// This is so that the model can be used immediately while the size computation is progress.
		MaxConcurrency: uint32(s.AdapterConfig.LimitModelConcurrency),
	}, nil
}

func (s *TorchServeAdapterServer) ModelSize(ctx context.Context, req *mmesh.ModelSizeRequest) (*mmesh.ModelSizeResponse, error) {
	log := s.Log.WithName("ModelSize").WithValues("modelId", req.ModelId)

	var size uint64
	var err error
	if size, err = modelSizeFromTorchServe(ctx, req.ModelId, s.ManagementClient, log); err != nil {
		log.Error(err, "Error determining model size via TorchServe DescribeModel call, using model disk size instead")
		if size, err = s.modelSizeFromDisk(req.ModelId); err != nil {
			return nil, err
		}
		log.Info("Returning model size computed from MAR file size on disk", "sizeInBytes", size)
	} else {
		log.Info("Returning model size retrieved from TorchServe worker used memory", "sizeInBytes", size)
	}
	return &mmesh.ModelSizeResponse{SizeInBytes: size}, nil
}

var newlineRegexp = regexp.MustCompile(`\n\s*`)

func modelSizeFromTorchServe(ctx context.Context, modelId string, client torchserve.ManagementAPIsServiceClient, log logr.Logger) (uint64, error) {
	descResp, err := client.DescribeModel(ctx, &torchserve.DescribeModelRequest{
		ModelName:    modelId,
		ModelVersion: "all", // Though version is optional here, torchserve currently doesn't interpret empty string as absent
	})
	if err != nil {
		return 0, fmt.Errorf("Failed to describe registered model: %w", err)
	}

	// Only deserialize fields we currently use
	type worker struct {
		//status      string
		//gpu         bool
		MemoryUsage int64
	}
	type modelDesc struct {
		//minWorkers, maxWorkers int
		Workers []worker
	}

	var memUsage uint64
	for i := 1; ; i += 1 {
		var descJson []modelDesc
		if err = json.Unmarshal([]byte(descResp.GetMsg()), &descJson); err != nil {
			return 0, fmt.Errorf("Failed to parse DescribeModel response: %w: %s", err, descResp.GetMsg())
		}

		compressedJson := newlineRegexp.ReplaceAllString(descResp.GetMsg(), " ")
		log.Info("DescribeModel response", "attempt", i, "message", compressedJson)

		if len(descJson) == 0 {
			return 0, fmt.Errorf("DescribeModel response contained no model entries")
		}
		if len(descJson) > 1 {
			log.Info("WARNING: More than one model version returned in DescribeModel response",
				"count", len(descJson))
		}
		if len(descJson[0].Workers) == 0 {
			return 0, fmt.Errorf("DescribeModel response contained no model worker entries")
		}

		// Sum reported memory usage of the workers
		memUsage = 0
		for _, w := range descJson[0].Workers {
			if w.MemoryUsage <= 0 {
				break
			}
			memUsage += uint64(w.MemoryUsage)
		}
		if memUsage > 0 {
			break
		}
		if i > 16 { // Give up after 16 seconds
			return 0, fmt.Errorf("No worker memory usage reported in DescribeModel responses")
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(1000 * time.Millisecond): // Try every second
		}
	}

	// Add 10% here to account for additional memory usage when running (TBD)
	memUsage += memUsage / 10

	return memUsage, nil
}

func (s *TorchServeAdapterServer) modelSizeFromDisk(modelId string) (uint64, error) {
	// Stat on symlink will give size of target file
	modelPath, _ := targetPathForModelId(modelId, s.AdapterConfig.ModelStoreDir)
	fi, err := os.Stat(modelPath)
	if err != nil {
		return 0, fmt.Errorf("Failed to determine size of model file on disk: %s: %w", modelPath, err)
	}
	return uint64(float64(fi.Size()) * s.AdapterConfig.ModelSizeMultiplier), nil
}

func targetPathForModelId(modelId, storeDir string) (fullPath, marFileName string) {
	// Encode \ as \\ and / as \_
	marFileName = fmt.Sprintf("%s.mar", strings.ReplaceAll(
		strings.ReplaceAll(modelId, `\`, `\\`), `/`, `\_`))
	fullPath = filepath.Join(storeDir, marFileName)
	return
}

// This creates a link in the torchserve model store dir to a model archive file retrieved
// by the puller (which will be in its own subdirectory)
func linkModelFile(modelPath, modelId, storeDir string, log logr.Logger) (string, error) {
	modelPathInfo, err := os.Stat(modelPath)
	if err != nil {
		return "", fmt.Errorf("Error calling stat on %s: %w", modelPath, err)
	}
	if modelPathInfo.IsDir() {
		return "", fmt.Errorf("Model path is a directory, must be a MAR file: %s", modelPath)
	}
	targetPath, marFileName := targetPathForModelId(modelId, storeDir)
	if err = clearModelLink(targetPath, log); err != nil {
		return "", fmt.Errorf("Error clearing path for model symlink: %w", err)
	}
	log.Info("Creating symlink", "from", modelPath, "to", targetPath)
	if err = os.Symlink(modelPath, targetPath); err != nil {
		return "", fmt.Errorf("Error creating symlink: %w", err)
	}
	return marFileName, nil
}

func clearModelLink(targetPath string, log logr.Logger) error {
	// clean up anything in our target location
	log.Info("Deleting path (if exists)", "path", targetPath)
	if err := os.RemoveAll(targetPath); err != nil {
		//TODO maybe should really return error here
		log.Info("Ignoring error trying to remove target", "targetPath", targetPath, "error", err)
	}
	return nil
}

func (s *TorchServeAdapterServer) UnloadModel(ctx context.Context, req *mmesh.UnloadModelRequest) (*mmesh.UnloadModelResponse, error) {
	log := s.Log.WithName("UnloadModel").WithValues("modelId", req.ModelId)
	unregResp, err := s.ManagementClient.UnregisterModel(ctx, &torchserve.UnregisterModelRequest{ModelName: req.ModelId})
	if err != nil {
		if grpcStatus, ok := status.FromError(err); ok && grpcStatus.Code() == codes.NotFound {
			// NotFound is generally unexpected, but fine
			log.Info("TorchServe returned NotFound from unregsister model request", "error", err)
		} else {
			log.Error(err, "Failed to unload model from TorchServe")
			return nil, status.Errorf(status.Code(err), "Failed to unload model from TorchServe")
		}
	} else {
		// Returned message looks like "Model \"mymodel\" unregistered"
		log.Info("TorchServe model unregistered successfully", "message", unregResp.GetMsg())
	}

	// delete symlink from runtime model store
	modelPath, _ := targetPathForModelId(req.ModelId, s.AdapterConfig.ModelStoreDir)
	if err = clearModelLink(modelPath, log); err != nil {
		return nil, status.Errorf(status.Code(err), "Error while deleting the symlink for model %s: %v", req.ModelId, err)
	}

	if s.AdapterConfig.UseEmbeddedPuller {
		// delete files from puller cache
		if err = s.Puller.CleanupModel(req.ModelId); err != nil {
			return nil, status.Errorf(status.Code(err), "Failed to delete model files from puller cache: %v", err)
		}
	}

	return &mmesh.UnloadModelResponse{}, nil
}

func (s *TorchServeAdapterServer) RuntimeStatus(ctx context.Context, req *mmesh.RuntimeStatusRequest) (*mmesh.RuntimeStatusResponse, error) {
	log := s.Log
	log.Info("runtimeStatus-entry")
	runtimeStatus := &mmesh.RuntimeStatusResponse{Status: mmesh.RuntimeStatusResponse_STARTING}

	mConnState := s.ManagementConn.GetState()
	if mConnState != connectivity.Idle && mConnState != connectivity.Ready {
		return runtimeStatus, nil
	}

	ic := s.InferenceClient
	if ic == nil {
		connCtx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()
		conn, err := grpc.DialContext(connCtx, s.InferenceEndpoint,
			grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock(),
			grpc.WithConnectParams(grpc.ConnectParams{Backoff: backoff.DefaultConfig, MinConnectTimeout: 10 * time.Second}))
		if err != nil {
			log.Error(err, "Cannot connect to TorchServe server inference endpoint",
				"endpoint", s.InferenceEndpoint)
			return runtimeStatus, nil
		}
		ic = torchserve.NewInferenceAPIsServiceClient(conn)
		s.InferenceConn = conn
		s.InferenceClient = ic
	}

	resp, tsErr := ic.Ping(ctx, &emptypb.Empty{})
	if tsErr != nil {
		log.Error(tsErr, "TorchServe ping failed", "response", resp)
		return runtimeStatus, nil
	}

	type healthStatus struct {
		Status string // "Healthy", "Partially Healthly", "Unhealthy"
	}

	var health healthStatus
	if err := json.Unmarshal([]byte(resp.Health), &health); err != nil {
		log.Error(err, "Failed to parse Ping response", "response", resp)
		return runtimeStatus, nil
	}

	if "Healthy" != health.Status {
		log.Info("TorchServe Ping returned non-Healthy status", "status", health.Status)
		return runtimeStatus, nil
	}

	// Clear any models from model server
	if err := unregisterAllModels(ctx, s.ManagementClient, log); err != nil {
		return runtimeStatus, nil
	}

	// Clear local symlinks
	if err := util.ClearDirectoryContents(s.AdapterConfig.ModelStoreDir, func(f os.DirEntry) bool {
		return strings.HasSuffix(f.Name(), ".mar")
	}); err != nil {
		log.Error(err, "Error cleaning up model store dir")
		return &mmesh.RuntimeStatusResponse{Status: mmesh.RuntimeStatusResponse_FAILING}, nil
	}

	if s.AdapterConfig.UseEmbeddedPuller {
		if err := s.Puller.ClearLocalModelStorage(torchServeModelStoreDirName); err != nil {
			log.Error(err, "Error cleaning up local model dir")
			return &mmesh.RuntimeStatusResponse{Status: mmesh.RuntimeStatusResponse_FAILING}, nil
		}
	}

	// Close inference connection now that we no longer need to ping
	s.InferenceConn.Close()
	s.InferenceClient = nil

	//if s.AdapterConfig.RuntimeVersion = "" {
	//	s.AdapterConfig.RuntimeVersion = //TODO maybe get from file in torchserve image
	//}

	runtimeStatus.Status = mmesh.RuntimeStatusResponse_READY
	runtimeStatus.CapacityInBytes = uint64(s.AdapterConfig.CapacityInBytes)
	runtimeStatus.MaxLoadingConcurrency = uint32(s.AdapterConfig.MaxLoadingConcurrency)
	runtimeStatus.ModelLoadingTimeoutMs = uint32(s.AdapterConfig.ModelLoadingTimeoutMS)
	runtimeStatus.DefaultModelSizeInBytes = uint64(s.AdapterConfig.DefaultModelSizeInBytes)
	runtimeStatus.RuntimeVersion = s.AdapterConfig.RuntimeVersion
	runtimeStatus.LimitModelConcurrency = s.AdapterConfig.LimitModelConcurrency > 0

	path1 := []uint32{1}

	mis := make(map[string]*mmesh.RuntimeStatusResponse_MethodInfo)
	mis[torchServeServiceName+"/Predictions"] = &mmesh.RuntimeStatusResponse_MethodInfo{IdInjectionPath: path1} // PredictionsRequest.model_name

	// TorchServe does not currently support KServe v2 *gRPC* API (only REST)
	// We are including the RPCs here however in the hope that such support will be added in future
	// - it will hopefully mean that things will then just "work" without any further code adjustments.
	// See https://github.com/pytorch/serve/issues/1859
	mis[KServeServiceName+"/ModelInfer"] = &mmesh.RuntimeStatusResponse_MethodInfo{IdInjectionPath: path1}
	mis[KServeServiceName+"/ModelMetadata"] = &mmesh.RuntimeStatusResponse_MethodInfo{IdInjectionPath: path1}

	runtimeStatus.MethodInfos = mis

	log.Info("runtimeStatus", "Status", runtimeStatus)
	return runtimeStatus, nil
}

func unregisterAllModels(ctx context.Context, client torchserve.ManagementAPIsServiceClient, log logr.Logger) error {
	type modelEntry struct {
		ModelName string
		//modelUrl  string // in json but not needed
	}
	type modelListResp struct {
		Models        []modelEntry
		NextPageToken int32
	}

	var modelList [][]modelEntry
	var token int32
	// Collect models to unregister
	for {
		listResp, err := client.ListModels(ctx, &torchserve.ListModelsRequest{
			Limit:         500,
			NextPageToken: token,
		})
		if err != nil {
			log.Error(err, "TorchServe list models failed", "response", listResp)
			return err
		}
		var respJson modelListResp
		if err := json.Unmarshal([]byte(listResp.GetMsg()), &respJson); err != nil {
			log.Error(err, "Failed to parse ModelList response", "response", listResp)
			return err
		}
		modelList = append(modelList, respJson.Models)
		if respJson.NextPageToken == 0 {
			break
		}
		token = respJson.NextPageToken
	}
	// Unregister all the models
	for _, models := range modelList {
		for _, model := range models {
			if _, err := client.UnregisterModel(ctx, &torchserve.UnregisterModelRequest{ModelName: model.ModelName}); err != nil {
				log.Error(err, "Unloading existing model failed", "modelId", model)
				return err
			}
		}
	}
	return nil
}
