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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc/credentials/insecure"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kserve/modelmesh-runtime-adapter/internal/modelschema"
	mlserver "github.com/kserve/modelmesh-runtime-adapter/internal/proto/mlserver/dataplane"
	modelrepo "github.com/kserve/modelmesh-runtime-adapter/internal/proto/mlserver/modelrepo"
	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
	"github.com/kserve/modelmesh-runtime-adapter/model-serving-puller/puller"
)

const (
	mlserverServiceName              string = "inference.GRPCInferenceService"
	mlserverModelSubdir              string = "_mlserver_models"
	mlserverRepositoryConfigFilename string = "model-settings.json"
)

type AdapterConfiguration struct {
	Port                         int
	MLServerPort                 int
	MLServerContainerMemReqBytes int
	MLServerMemBufferBytes       int
	CapacityInBytes              int
	MaxLoadingConcurrency        int
	ModelLoadingTimeoutMS        int
	DefaultModelSizeInBytes      int
	ModelSizeMultiplier          float64
	RuntimeVersion               string
	LimitModelConcurrency        int // 0 means no limit (default)
	RootModelDir                 string
	UseEmbeddedPuller            bool
}

type MLServerAdapterServer struct {
	Client          mlserver.GRPCInferenceServiceClient
	ModelRepoClient modelrepo.ModelRepositoryServiceClient
	Conn            *grpc.ClientConn
	Puller          *puller.Puller
	AdapterConfig   *AdapterConfiguration
	Log             logr.Logger

	// embed generated Unimplemented type for forward-compatibility for gRPC
	mmesh.UnimplementedModelRuntimeServer
}

func NewMLServerAdapterServer(runtimePort int, config *AdapterConfiguration, log logr.Logger) *MLServerAdapterServer {
	log = log.WithName("MLServer Adapter Server")
	log.Info("Connecting to MLServer...", "port", runtimePort)

	mlserverClientCtx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	conn, err := grpc.DialContext(mlserverClientCtx, fmt.Sprintf("localhost:%d", runtimePort),
		grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock(),
		grpc.WithConnectParams(grpc.ConnectParams{Backoff: backoff.DefaultConfig, MinConnectTimeout: 10 * time.Second}))

	if err != nil {
		log.Error(err, "Can not connect to MLServer runtime")
		os.Exit(1)
	}

	s := new(MLServerAdapterServer)
	s.Log = log
	s.AdapterConfig = config
	s.Client = mlserver.NewGRPCInferenceServiceClient(conn)
	s.Conn = conn
	s.ModelRepoClient = modelrepo.NewModelRepositoryServiceClient(conn)
	if s.AdapterConfig.UseEmbeddedPuller {
		// puller is configured from its own env vars
		s.Puller = puller.NewPuller(log)
	}

	resp, mlserverErr := s.Client.ServerMetadata(mlserverClientCtx, &mlserver.ServerMetadataRequest{})

	if mlserverErr != nil || resp.Version == "" {
		log.Error(mlserverErr, "MLServer failed to get server metadata")
	} else {
		s.AdapterConfig.RuntimeVersion = resp.Version
	}

	log.Info("MLServer Runtime connected!")

	return s
}

func (s *MLServerAdapterServer) LoadModel(ctx context.Context, req *mmesh.LoadModelRequest) (*mmesh.LoadModelResponse, error) {
	log := s.Log.WithName("Load Model")
	log = log.WithValues("model_id", req.ModelId)
	modelType := util.GetModelType(req, log)
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

	// create a file layout from the files downloaded by the puller that can be loaded by the runtime
	err = adaptModelLayoutForRuntime(s.AdapterConfig.RootModelDir, req.ModelId, modelType, req.ModelPath, schemaPath, log)
	if err != nil {
		log.Error(err, "Failed to create model directory and load model")
		return nil, status.Errorf(status.Code(err), "Failed to load Model due to adapter error: %s", err)
	}

	_, mlserverErr := s.ModelRepoClient.RepositoryModelLoad(ctx, &modelrepo.RepositoryModelLoadRequest{
		ModelName: req.ModelId,
	})
	if mlserverErr != nil {
		log.Error(mlserverErr, "MLServer failed to load model")
		return nil, status.Errorf(status.Code(mlserverErr), "Failed to load Model due to MLServer runtime error: %s", mlserverErr)
	}

	size := util.CalcMemCapacity(req.ModelKey, s.AdapterConfig.DefaultModelSizeInBytes, s.AdapterConfig.ModelSizeMultiplier, log)

	log.Info("MLServer model loaded")

	return &mmesh.LoadModelResponse{
		SizeInBytes:    size,
		MaxConcurrency: uint32(s.AdapterConfig.LimitModelConcurrency),
	}, nil
}

// adaptModelLayoutForRuntime creates a directory that can be loaded by the runtime from the files downloaded by the puller
func adaptModelLayoutForRuntime(rootModelDir, modelID, modelType, modelPath, schemaPath string, log logr.Logger) error {
	// convert to lower case and remove anything after a :
	modelType = strings.ToLower(strings.Split(modelType, ":")[0])

	mlserverModelIDDir, err := util.SecureJoin(rootModelDir, mlserverModelSubdir, modelID)
	if err != nil {
		log.Error(err, "Unable to securely join", "rootModelDir", rootModelDir, "mlserverModelSubdir", mlserverModelSubdir, "modelID", modelID)
		return err
	}

	// clean up and then create directory where the rewritten model repo will live
	if removeErr := os.RemoveAll(mlserverModelIDDir); removeErr != nil {
		log.Info("Ignoring error trying to remove dir", "Directory", mlserverModelIDDir, "Error", removeErr)
	}
	if mkdirErr := os.MkdirAll(mlserverModelIDDir, 0755); mkdirErr != nil {
		return fmt.Errorf("Error creating directories for path %s: %w", mlserverModelIDDir, mkdirErr)
	}

	// if modelPath references a directory and contains the config file, we
	// assume files are in the "native" repo structure
	// otherwise, we attempt to adapt the model files to be loaded by MLServer
	modelPathInfo, err := os.Stat(modelPath)
	if err != nil {
		return fmt.Errorf("Error calling stat on %s: %w", modelPath, err)
	}

	if !modelPathInfo.IsDir() {
		// simpler case if ModelPath points to a file
		err = adaptModelLayout(modelID, modelType, modelPath, schemaPath, mlserverModelIDDir, log)
	} else {
		// model path is a directory, inspect the files
		files, err1 := ioutil.ReadDir(modelPath)
		if err1 != nil {
			return fmt.Errorf("Could not read files in dir %s: %w", modelPath, err)
		}
		// check if the config file exists
		// if it does, we assume files are in the "native" repo structure
		assumeNativeLayout := false
		for _, f := range files {
			if f.Name() == mlserverRepositoryConfigFilename {
				assumeNativeLayout = true
				break
			}
		}
		if assumeNativeLayout {
			err = adaptNativeModelLayout(files, modelID, modelPath, schemaPath, mlserverModelIDDir, log)
		} else {
			err = adaptModelLayout(modelID, modelType, modelPath, schemaPath, mlserverModelIDDir, log)
		}
	}
	if err != nil {
		return fmt.Errorf("Error adapting model directory %s: %w", modelPath, err)
	}
	return nil
}

// adaptNativeModelLayout mostly passes the model through to the runtime
//
// Only minimal changes should be made to the model repo to get it to load. For
// MLServer, this means writing the model ID into the configuration file and
// just symlinking all other files
func adaptNativeModelLayout(files []os.FileInfo, modelID, modelPath, schemaPath, targetDir string, log logr.Logger) error {
	for _, f := range files {
		filename := f.Name()
		source, err := util.SecureJoin(modelPath, filename)
		if err != nil {
			log.Error(err, "Unable to securely join", "sourceDir", modelPath, "filename", filename)
			return err
		}
		// special handling of the config file
		if filename == mlserverRepositoryConfigFilename {
			configJSON, err1 := ioutil.ReadFile(source)
			if err1 != nil {
				return fmt.Errorf("Could not read model config file %s: %w", source, err1)
			}

			// process the config to set the model's `name` to the model-mesh model id
			processedConfigJSON, err1 := processConfigJSON(configJSON, modelID, targetDir, schemaPath, log)
			if err1 != nil {
				return fmt.Errorf("Error processing config file %s: %w", source, err1)
			}

			target, jerr := util.SecureJoin(targetDir, mlserverRepositoryConfigFilename)
			if jerr != nil {
				log.Error(jerr, "Unable to securely join", "targetDir", targetDir, "mlserverRepositoryConfigFilename", mlserverRepositoryConfigFilename)
				return jerr
			}
			err1 = ioutil.WriteFile(target, processedConfigJSON, f.Mode())
			if err1 != nil {
				return fmt.Errorf("Error writing config file %s: %w", source, err1)
			}
			continue
		}
		// symlink all other entries
		link, err := util.SecureJoin(targetDir, filename)
		if err != nil {
			log.Error(err, "Unable to securely join", "targetDir", targetDir, "filename", filename)
			return err
		}
		err = os.Symlink(source, link)
		if err != nil {
			return fmt.Errorf("Error creating symlink to %s: %w", source, err)
		}
	}

	return nil
}

// processConfigJson sets `name` field in the model config
//
// Returns bytes with the bytes of the processed config. MLServer requires the
// name parameter to exist and be equal to the model id. The directory is
// ignored.
func processConfigJSON(jsonIn []byte, modelID string, targetDir string, schemaPath string, log logr.Logger) ([]byte, error) {
	// parse the json as a map
	var j map[string]interface{}
	if err := json.Unmarshal(jsonIn, &j); err != nil {
		log.Info("Unable to unmarshal config file", "modelID", modelID, "error", err)
		// return the input and hope for the best
		return jsonIn, nil
	}
	// set the name field
	j["name"] = modelID
	// rewrite uri from relative to absolute path
	if j["parameters"] != nil && j["parameters"].(map[string]interface{})["uri"] != nil {
		uri := j["parameters"].(map[string]interface{})["uri"].(string)
		var err error
		j["parameters"].(map[string]interface{})["uri"], err = util.SecureJoin(targetDir, uri)
		if err != nil {
			log.Info("Error joining paths", "directory", targetDir, "uri", uri, "error", err)
			return jsonIn, nil
		}
	}

	if schemaPath != "" {
		s, err1 := modelschema.NewFromFile(schemaPath)
		if err1 != nil {
			return nil, fmt.Errorf("Error parsing schema file: %w", err1)
		}
		if err1 = processSchema(j, s); err1 != nil {
			return nil, fmt.Errorf("Error processing schema file: %w", err1)
		}
	}

	jsonOut, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		log.Info("Unable to marshal config file", "modelID", modelID, "error", err)
		// return the input and hope for the best
		return jsonIn, nil
	}

	return jsonOut, nil
}

// adaptModelLayout attempts to generate a functional repo structure from the input files
//
// - generate a model settings file
// - inject schema information if schemaPath is included
// - use symlinks to reference files from the source modelPath
// - use modelPath to construct the model's URI as an absolute path
func adaptModelLayout(modelID, modelType, modelPath, schemaPath, targetDir string, log logr.Logger) error {
	// soft-link to either directory or file depending on the input received
	linkPath, err := util.SecureJoin(targetDir, filepath.Base(modelPath))
	if err != nil {
		log.Error(err, "Unable to securely join", "targetDir", targetDir, "filename", filepath.Base(modelPath))
		return err
	}

	err = os.Symlink(modelPath, linkPath)
	if err != nil {
		return fmt.Errorf("Error creating symlink: %w", err)
	}

	// generate the required configuration file
	configJSON, err := generateModelConfigJSON(modelID, modelType, linkPath, schemaPath)
	if err != nil {
		return fmt.Errorf("Error generating config file for %s: %w", modelID, err)
	}

	target, err := util.SecureJoin(targetDir, mlserverRepositoryConfigFilename)
	if err != nil {
		log.Error(err, "Unable to securely join", "targetDir", targetDir, "mlserverRepositoryConfigFilename", mlserverRepositoryConfigFilename)
		return err
	}
	err = ioutil.WriteFile(target, configJSON, 0664)
	if err != nil {
		return fmt.Errorf("Error writing generated config file for %s: %w", modelID, err)
	}

	return nil
}

func generateModelConfigJSON(modelID string, modelType string, uri string, schemaPath string) ([]byte, error) {
	j := make(map[string]interface{})
	// set the name
	j["name"] = modelID

	// set the implementation based on the model type
	modelTypeToImplementationMapping := map[string]string{
		"lightgbm": "mlserver_lightgbm.LightGBMModel",
		"sklearn":  "mlserver_sklearn.SKLearnModel",
		"xgboost":  "mlserver_xgboost.XGBoostModel",
		"mllib":    "mlserver-mllib.MLlibModel",
	}

	// set the implementation
	if imp := modelTypeToImplementationMapping[modelType]; imp != "" {
		j["implementation"] = imp
	}
	j["parameters"] = map[string]interface{}{"uri": uri}
	// unexpected model type, just leave out the implementation field

	// TODO: The mllib model type supports mulitple classes of model in MLServer
	// and requires the parameters.format field to be set. We can't determine what
	// that should be.
	// REF: https://github.com/SeldonIO/MLServer/blob/8de07ba14153391acc0167442fbc8df7705da0e3/mlserver/utils.py#L46-L54
	//
	// If we add support for determining the format, we would also set
	// parameters.uri to "./" as the default path
	// REF: https://github.com/SeldonIO/MLServer/blob/7c0e98e14d128dba2d91a225038bad4b974efac0/runtimes/mllib/mlserver_mllib/utils.py#L36-L46

	if schemaPath != "" {
		s, err1 := modelschema.NewFromFile(schemaPath)
		if err1 != nil {
			return nil, fmt.Errorf("Error parsing schema file: %w", err1)
		}
		if err1 = processSchema(j, s); err1 != nil {
			return nil, fmt.Errorf("Error processing schema file: %w", err1)
		}
	}

	jsonOut, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("Unable to marshal JSON: %w", err)
	}

	return jsonOut, nil
}

func processSchema(c map[string]interface{}, s *modelschema.ModelSchema) error {
	if s.Inputs != nil {
		inputs := make([]interface{}, len(s.Inputs))
		for i, m := range s.Inputs {
			inputs[i] = tensorMetadataToJson(m)
		}
		c["inputs"] = inputs
	}

	if s.Outputs != nil {
		outputs := make([]interface{}, len(s.Outputs))
		for i, m := range s.Outputs {
			outputs[i] = tensorMetadataToJson(m)
		}
		c["outputs"] = outputs
	}

	return nil
}

func tensorMetadataToJson(tm modelschema.TensorMetadata) map[string]interface{} {
	json := make(map[string]interface{})
	json["name"] = tm.Name
	json["datatype"] = tm.Datatype
	json["shape"] = tm.Shape

	return json
}

func (s *MLServerAdapterServer) UnloadModel(ctx context.Context, req *mmesh.UnloadModelRequest) (*mmesh.UnloadModelResponse, error) {
	_, mlserverErr := s.ModelRepoClient.RepositoryModelUnload(ctx, &modelrepo.RepositoryModelUnloadRequest{
		ModelName: req.ModelId,
	})

	if mlserverErr != nil {
		// check if we got a gRPC error as a response that indicates that MLServer
		// does not have the model registered. In that case we still want to proceed
		// with removing the model files.
		// Currently, MLServer returns an INVALID_ARGUMENT status if the model
		// doesn't exist, but this may become NOT_FOUND in the future.
		if grpcStatus, ok := status.FromError(mlserverErr); ok &&
			(grpcStatus.Code() == codes.InvalidArgument || grpcStatus.Code() == codes.NotFound) {

			s.Log.Info("Unload request for model not found in MLServer", "error", mlserverErr, "model_id", req.ModelId)
		} else {
			s.Log.Error(mlserverErr, "Failed to unload model from MLServer", "model_id", req.ModelId)
			return nil, status.Errorf(status.Code(mlserverErr), "Failed to unload model from MLServer")
		}
	}

	// delete files from runtime model repository
	mlserverModelIDDir, err := util.SecureJoin(s.AdapterConfig.RootModelDir, mlserverModelSubdir, req.ModelId)
	if err != nil {
		s.Log.Error(err, "Unable to securely join", "rootModelDir", s.AdapterConfig.RootModelDir, "mlserverModelSubdir", mlserverModelSubdir, "modelId", req.ModelId)
		return nil, err
	}
	err = os.RemoveAll(mlserverModelIDDir)
	if err != nil {
		return nil, status.Errorf(status.Code(err), "Error while deleting the %s dir: %v", mlserverModelIDDir, err)
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

func (s *MLServerAdapterServer) RuntimeStatus(ctx context.Context, req *mmesh.RuntimeStatusRequest) (*mmesh.RuntimeStatusResponse, error) {
	log := s.Log
	runtimeStatus := new(mmesh.RuntimeStatusResponse)

	log.Info("runtimeStatus-entry")
	serverReadyResponse, mlserverErr := s.Client.ServerReady(ctx, &mlserver.ServerReadyRequest{})

	if mlserverErr != nil {
		log.Info("MLServer failed to get status or not ready", "error", mlserverErr)
		runtimeStatus.Status = mmesh.RuntimeStatusResponse_STARTING
		return runtimeStatus, nil
	}

	if !serverReadyResponse.Ready {
		log.Info("MLServer runtime not ready")
		runtimeStatus.Status = mmesh.RuntimeStatusResponse_STARTING
		return runtimeStatus, nil
	}

	//unloading if there are any models already loaded
	indexResponse, mlserverErr := s.ModelRepoClient.RepositoryIndex(ctx, &modelrepo.RepositoryIndexRequest{
		RepositoryName: "",
		Ready:          true})

	if mlserverErr != nil {
		runtimeStatus.Status = mmesh.RuntimeStatusResponse_STARTING
		log.Info("MLServer runtime status, getting model info failed", "error", mlserverErr)
		return runtimeStatus, nil
	}

	for model := range indexResponse.Models {
		_, mlserverErr := s.ModelRepoClient.RepositoryModelUnload(ctx, &modelrepo.RepositoryModelUnloadRequest{
			ModelName: indexResponse.Models[model].Name,
		})

		if mlserverErr != nil {
			runtimeStatus.Status = mmesh.RuntimeStatusResponse_STARTING
			s.Log.Info("MLServer runtime status, unload model failed", "error", mlserverErr)
			return runtimeStatus, nil
		}
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

	mis[mlserverServiceName+"/ModelInfer"] = &mmesh.RuntimeStatusResponse_MethodInfo{IdInjectionPath: path1}
	mis[mlserverServiceName+"/ModelMetadata"] = &mmesh.RuntimeStatusResponse_MethodInfo{IdInjectionPath: path1}
	runtimeStatus.MethodInfos = mis

	log.Info("runtimeStatus", "Status", runtimeStatus)
	return runtimeStatus, nil
}
