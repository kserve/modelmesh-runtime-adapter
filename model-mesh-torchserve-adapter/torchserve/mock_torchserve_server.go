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

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/torchserve"

	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	mock "github.com/kserve/modelmesh-runtime-adapter/model-mesh-torchserve-adapter/generated/mocks"
	"google.golang.org/grpc"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// wrap unimplemented structs to embed deeper than the mocks
type unimplementedServer struct {
	torchserve.UnimplementedInferenceAPIsServiceServer
	torchserve.UnimplementedManagementAPIsServiceServer
}

type mockInferenceAPIsServiceWithUnimplemented struct {
	*mock.MockInferenceAPIsServiceServer
	unimplementedServer
}

type mockManagementApisServiceWithUnimplemented struct {
	*mock.MockManagementAPIsServiceServer
	unimplementedServer
}

func main() {
	log := zap.New(zap.UseDevMode(true))

	// custom TestReporter to pass go gomock
	cr := CustomReporter{log}
	ctrl := gomock.NewController(cr)
	defer ctrl.Finish()

	log.Info("Mock TorchServe gRPC Server Registered...")
	iServer, mServer := grpc.NewServer(), grpc.NewServer()

	// Mock InferenceAPIs Service
	i := mock.NewMockInferenceAPIsServiceServer(ctrl)
	i.EXPECT().Ping(gomock.Any(), gomock.Any()).AnyTimes().Return(&torchserve.TorchServeHealthResponse{
		Health: `{"status": "Healthy"}`,
	}, nil)

	torchserve.RegisterInferenceAPIsServiceServer(iServer, mockInferenceAPIsServiceWithUnimplemented{i, unimplementedServer{}})

	// Mock ManagementAPIs Service

	m := mock.NewMockManagementAPIsServiceServer(ctrl)
	//m.EXPECT().DescribeModel(gomock.Any(), gomock.Any()).Times(1).Return(&torchserve.ManagementResponse{
	//	Msg: `[{"workers":[{"memoryUsage":0}]}]`,
	//}, nil)
	first := m.EXPECT().DescribeModel(gomock.Any(), gomock.Any()).Return(&torchserve.ManagementResponse{
		Msg: `[invalid json]`, // return invalid json the first time so that model size on disk (fallback) is used instead
	}, nil)
	second := m.EXPECT().DescribeModel(gomock.Any(), gomock.Any()).Return(&torchserve.ManagementResponse{
		Msg: `[{"workers":[{"memoryUsage":11111}]}]`,
	}, nil)
	gomock.InOrder(first, second)
	m.EXPECT().ListModels(gomock.Any(), gomock.Any()).AnyTimes().Return(&torchserve.ManagementResponse{Msg: "{}"}, nil)
	m.EXPECT().RegisterModel(gomock.Any(), gomock.Any()).AnyTimes().Return(&torchserve.ManagementResponse{
		Msg: `Model "test-pt-model" Version: 1.0 registered with 1 initial workers`,
	}, nil)
	m.EXPECT().UnregisterModel(gomock.Any(), gomock.Any()).AnyTimes().Return(&torchserve.ManagementResponse{
		Msg: `Model "test-pt-model" unregistered`,
	}, nil)

	torchserve.RegisterManagementAPIsServiceServer(mServer, mockManagementApisServiceWithUnimplemented{m, unimplementedServer{}})

	go serve(mServer, 7071, log)
	serve(iServer, 7070, log)
}

func serve(server *grpc.Server, port uint16, log logr.Logger) {
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		log.Error(err, "*** Mock TorchServe server failed to listen port")
		return
	}

	if err := server.Serve(lis); err != nil {
		log.Error(err, "*** Mock TorchServe terminated with error: ")
	} else {
		log.Info("*** Mock TorchServe terminated")
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
