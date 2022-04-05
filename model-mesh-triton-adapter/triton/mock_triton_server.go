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
package main

import (
	"fmt"
	"net"
	"os"

	triton "github.com/kserve/modelmesh-runtime-adapter/internal/proto/triton"
	mock "github.com/kserve/modelmesh-runtime-adapter/model-mesh-triton-adapter/generated/mocks"
	"google.golang.org/grpc"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// wrap unimplemented struct to embed deeper than the mock
type unimplementedServer struct {
	triton.UnimplementedGRPCInferenceServiceServer
}

type mockInferenceServiceWithUnimplemented struct {
	*mock.MockGRPCInferenceServiceServer
	unimplementedServer
}

func main() {
	log := zap.New(zap.UseDevMode(true))
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", 8001))

	if err != nil {
		log.Error(err, "*** Mock Triton server failed to listen port")
		os.Exit(1)
	}

	var opts []grpc.ServerOption

	log.Info("Mock Triton gRPC Server Registered...")
	grpcServer := grpc.NewServer(opts...)

	triton.RegisterGRPCInferenceServiceServer(grpcServer, &mockInferenceServiceWithUnimplemented{})

	err = grpcServer.Serve(lis)

	if err != nil {
		log.Info("*** Mock Triton terminated with error: ", "error", err)
	} else {
		log.Info("*** Mock Triton terminated")
	}
}
