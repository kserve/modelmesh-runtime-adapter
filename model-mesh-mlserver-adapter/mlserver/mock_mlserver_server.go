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
	"errors"
	"fmt"
	"net"

	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	mlserver "github.com/kserve/modelmesh-runtime-adapter/internal/proto/mlserver/dataplane"
	modelrepo "github.com/kserve/modelmesh-runtime-adapter/internal/proto/mlserver/modelrepo"
	mock "github.com/kserve/modelmesh-runtime-adapter/model-mesh-mlserver-adapter/generated/mocks"
	"google.golang.org/grpc"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// wrap unimplemented structs to embed deeper than the mocks
type unimplementedServer struct {
	mlserver.UnimplementedGRPCInferenceServiceServer
	modelrepo.UnimplementedModelRepositoryServiceServer
}

type mockInferenceServiceWithUnimplemented struct {
	*mock.MockGRPCInferenceServiceServer
	unimplementedServer
}

type mockModelRepoServiceWithUnimplemented struct {
	*mock.MockModelRepositoryServiceServer
	unimplementedServer
}

func main() {
	log := zap.New(zap.UseDevMode(true))
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", 8001))

	if err != nil {
		log.Error(err, "*** Mock MLServer server failed to listen port")
	}

	// custom TestReporter to pass go gomock
	cr := CustomReporter{log}
	ctrl := gomock.NewController(cr)
	defer ctrl.Finish()

	var opts []grpc.ServerOption

	log.Info("Mock MLServer gRPC Server Registered...")
	grpcServer := grpc.NewServer(opts...)

	// Mock Inference Service
	i := mock.NewMockGRPCInferenceServiceServer(ctrl)
	i.EXPECT().ServerLive(gomock.Any(), gomock.Any()).AnyTimes().Return(&mlserver.ServerLiveResponse{Live: true}, nil)
	i.EXPECT().ServerReady(gomock.Any(), gomock.Any()).AnyTimes().Return(&mlserver.ServerReadyResponse{Ready: true}, nil)
	i.EXPECT().ServerMetadata(gomock.Any(), gomock.Any()).AnyTimes().Return(&mlserver.ServerMetadataResponse{}, nil)

	mlserver.RegisterGRPCInferenceServiceServer(grpcServer, mockInferenceServiceWithUnimplemented{i, unimplementedServer{}})

	// Mock ModelRepository Service

	mr := mock.NewMockModelRepositoryServiceServer(ctrl)
	mr.EXPECT().RepositoryIndex(gomock.Any(), gomock.Any()).AnyTimes().Return(&modelrepo.RepositoryIndexResponse{}, nil)
	mr.EXPECT().RepositoryModelLoad(gomock.Any(), gomock.Any()).AnyTimes().Return(&modelrepo.RepositoryModelLoadResponse{}, nil)
	mr.EXPECT().RepositoryModelUnload(gomock.Any(), gomock.Any()).AnyTimes().Return(&modelrepo.RepositoryModelUnloadResponse{}, nil)

	modelrepo.RegisterModelRepositoryServiceServer(grpcServer, mockModelRepoServiceWithUnimplemented{mr, unimplementedServer{}})

	err = grpcServer.Serve(lis)

	if err != nil {
		log.Info("*** Mock MLServer terminated with error: ", "error", err)
	} else {
		log.Info("*** Mock MLServer terminated")
	}
}

// Implements gomock TestReporter interface:
//
// type TestReporter interface {
// 	Errorf(format string, args ...interface{})
// 	Fatalf(format string, args ...interface{})
// }
type CustomReporter struct {
	log logr.Logger
}

func (cr CustomReporter) Errorf(format string, args ...interface{}) {
	errString := fmt.Sprintf(format, args...)
	err := errors.New(errString)
	cr.log.Error(err, errString)
}
func (cr CustomReporter) Fatalf(format string, args ...interface{}) {
	errString := fmt.Sprintf(format, args...)
	err := errors.New(errString)
	cr.log.Error(err, errString)
	// gomock expects Fatalf to stop execution
	// Without the panic here, a SIGSEGV occurs instead...
	// Ref: https://github.com/golang/mock/issues/425
	panic(errString)
}
