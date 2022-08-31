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
	"fmt"
	"net"
	"os"
	"time"

	"github.com/kserve/modelmesh-runtime-adapter/internal/util"

	"google.golang.org/grpc/credentials/insecure"

	"github.com/go-logr/logr"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	"github.com/kserve/modelmesh-runtime-adapter/model-serving-puller/puller"
)

var PurgeExcludePrefixes = []string{"_"}

// PullerServer represents the GRPC server and its configuration
type PullerServer struct {
	modelRuntimeClient mmesh.ModelRuntimeClient
	Log                logr.Logger
	pullerServerConfig *PullerServerConfiguration
	puller             *puller.Puller
	sm                 *modelStateManager

	// embed generated Unimplemented type for forward-compatibility for gRPC
	mmesh.UnimplementedModelRuntimeServer
}

// NewPullerServer creates a new PullerServer instance and initializes it with configuration from the environment
func NewPullerServer(log logr.Logger) *PullerServer {
	return NewPullerServerFromConfig(log, GetPullerServerConfigFromEnv(log))
}

// NewPullerServerFromConfig creates a new PullerServer instance with the given configuration
func NewPullerServerFromConfig(log logr.Logger, config *PullerServerConfiguration) *PullerServer {
	s := new(PullerServer)
	s.Log = log
	s.pullerServerConfig = config
	s.puller = puller.NewPuller(log)

	s.sm, _ = newModelStateManager(log, s)
	return s
}

// StartServer runs the gRPC server. This func will not return unless the server fails.
func (s *PullerServer) StartServer() error {
	log := s.Log
	log.Info("Starting puller server", "puller_config", s.pullerServerConfig)

	// Create a connection to the model runtime gRPC server
	log.Info("Connecting to model runtime", "endpoint", s.pullerServerConfig.ModelServerEndpoint)
	runtimeClientCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	modelServerEndpoint, err := util.ResolveLocalGrpcEndpoint(s.pullerServerConfig.ModelServerEndpoint)
	if err != nil {
		return err
	}
	modelRuntimeConnection, err := grpc.DialContext(
		runtimeClientCtx,
		modelServerEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff:           backoff.DefaultConfig,
			MinConnectTimeout: 2 * time.Second,
		}))
	if err != nil {
		log.Error(err, "Cannot connect to runtime")
		os.Exit(1)
	}
	defer modelRuntimeConnection.Close()
	s.modelRuntimeClient = mmesh.NewModelRuntimeClient(modelRuntimeConnection)
	log.Info("Model runtime connected!")

	// Setup this server
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", s.pullerServerConfig.Port))
	if err != nil {
		log.Error(err, "*** Server failed to listen on port", "port", s.pullerServerConfig.Port)
		os.Exit(1)
	}
	log.Info("Puller will run at port", "port", s.pullerServerConfig.Port)
	grpcServer := grpc.NewServer()
	mmesh.RegisterModelRuntimeServer(grpcServer, s)
	log.Info("gRPC Server Registered...")
	err = grpcServer.Serve(lis)
	return err
}

// LoadModel loads a model and returns when model is fully loaded.
// See model-runtime.proto loadModel()
func (s *PullerServer) LoadModel(ctx context.Context, req *mmesh.LoadModelRequest) (*mmesh.LoadModelResponse, error) {
	s.Log.Info("Enqueuing loading of the model")
	return s.sm.loadModel(ctx, req)
}

func (s *PullerServer) loadModel(ctx context.Context, req *mmesh.LoadModelRequest) (*mmesh.LoadModelResponse, error) {
	log := s.Log
	log = log.WithValues("model_id", req.ModelId, "model_path", req.ModelPath, "model_key", req.ModelKey, "model_type", req.ModelType)
	log.Info("Loading model")

	// Pull the model from storage
	var pullerErr error
	req, pullerErr = s.puller.ProcessLoadModelRequest(ctx, req)
	if pullerErr != nil {
		log.Error(pullerErr, "Failed to pull model from storage")
		return nil, pullerErr
	}

	// Call model runtime grpc loadModel
	response, err := s.modelRuntimeClient.LoadModel(ctx, req)
	if err != nil {
		log.Error(err, "Model runtime failed to load model", "model_id", req.ModelId)
		return nil, status.Errorf(status.Code(err), "Failed to load model due to model runtime error: %s", err)
	}

	return response, nil
}

// UnloadModel unloads a previously loaded (or failed) model and returns when model
// is fully unloaded, or immediately if not found/loaded.
// See model-runtime.proto unloadModel()
func (s *PullerServer) UnloadModel(ctx context.Context, req *mmesh.UnloadModelRequest) (*mmesh.UnloadModelResponse, error) {
	s.Log.Info("Enqueuing unloading of the model")
	return s.sm.unloadModel(ctx, req)
}

func (s *PullerServer) unloadModel(ctx context.Context, req *mmesh.UnloadModelRequest) (*mmesh.UnloadModelResponse, error) {
	log := s.Log.WithValues("model_id", req.ModelId)
	log.Info("Unloading model")
	// First unload the model from the runtime.
	unloadResponse, err := s.modelRuntimeClient.UnloadModel(ctx, req)
	if err != nil {
		// check if we got a gRPC error as a response that indicates that the runtime
		// does not have the model registered. In that case we still want to proceed
		// with removing the model files.
		if grpcStatus, ok := status.FromError(err); ok && grpcStatus.Code() == codes.NotFound {
			log.Info("Unload request for model not found in the runtime", "error", err)
		} else {
			log.Error(err, "Failed to unload model from runtime")
			return unloadResponse, status.Errorf(status.Code(err), "Failed to unload model from runtime")
		}
	}

	// Now delete the local file
	err = s.puller.CleanupModel(req.ModelId)
	if err != nil {
		return nil, status.Errorf(status.Code(err), "Failed to delete model from local filesystem: %s", err)
	}

	return &mmesh.UnloadModelResponse{}, nil
}

// PredictModelSize predicts the size of not-yet-loaded model - must return almost immediately.
// See model-runtime.proto predictModelSize()
// This is a Direct passthrough to the model runtime grpc
func (s *PullerServer) PredictModelSize(ctx context.Context, req *mmesh.PredictModelSizeRequest) (*mmesh.PredictModelSizeResponse, error) {
	s.Log.Info("Predicting model size", "model_id", req.ModelId, "model_path", req.ModelPath, "model_key", req.ModelKey, "model_type", req.ModelType)
	return s.modelRuntimeClient.PredictModelSize(ctx, req)
}

// ModelSize calculates the size (memory consumption) of a currently-loaded model.
// See model-runtime.proto modelSize()
// This is a Direct passthrough to the model runtime grpc
func (s *PullerServer) ModelSize(ctx context.Context, req *mmesh.ModelSizeRequest) (*mmesh.ModelSizeResponse, error) {
	s.Log.Info("Getting model size", "model_id", req.ModelId)
	return s.modelRuntimeClient.ModelSize(ctx, req)
}

// RuntimeStatus provides basic runtime status and parameters; called only during startup.
// This is a Direct passthrough to the model runtime grpc
// See model-runtime.proto runtimeStatus()
func (s *PullerServer) RuntimeStatus(ctx context.Context, req *mmesh.RuntimeStatusRequest) (*mmesh.RuntimeStatusResponse, error) {
	s.Log.Info("Getting runtime status")

	rsr, err := s.modelRuntimeClient.RuntimeStatus(ctx, req)
	if err != nil || rsr.Status != mmesh.RuntimeStatusResponse_READY {
		return rsr, err
	}

	s.Log.Info("Unloading all prior loaded models to return to zero state")
	if err := s.sm.unloadAll(); err != nil {
		s.Log.Error(err, "Error unloading all models")

		return &mmesh.RuntimeStatusResponse{Status: mmesh.RuntimeStatusResponse_FAILING}, err
	}

	return rsr, nil // READY
}
