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
// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh (interfaces: ModelRuntimeClient)

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	mmesh "github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	grpc "google.golang.org/grpc"
)

// MockModelRuntimeClient is a mock of ModelRuntimeClient interface.
type MockModelRuntimeClient struct {
	ctrl     *gomock.Controller
	recorder *MockModelRuntimeClientMockRecorder
}

// MockModelRuntimeClientMockRecorder is the mock recorder for MockModelRuntimeClient.
type MockModelRuntimeClientMockRecorder struct {
	mock *MockModelRuntimeClient
}

// NewMockModelRuntimeClient creates a new mock instance.
func NewMockModelRuntimeClient(ctrl *gomock.Controller) *MockModelRuntimeClient {
	mock := &MockModelRuntimeClient{ctrl: ctrl}
	mock.recorder = &MockModelRuntimeClientMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockModelRuntimeClient) EXPECT() *MockModelRuntimeClientMockRecorder {
	return m.recorder
}

// LoadModel mocks base method.
func (m *MockModelRuntimeClient) LoadModel(arg0 context.Context, arg1 *mmesh.LoadModelRequest, arg2 ...grpc.CallOption) (*mmesh.LoadModelResponse, error) {
	m.ctrl.T.Helper()
	varargs := []interface{}{arg0, arg1}
	for _, a := range arg2 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "LoadModel", varargs...)
	ret0, _ := ret[0].(*mmesh.LoadModelResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// LoadModel indicates an expected call of LoadModel.
func (mr *MockModelRuntimeClientMockRecorder) LoadModel(arg0, arg1 interface{}, arg2 ...interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]interface{}{arg0, arg1}, arg2...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LoadModel", reflect.TypeOf((*MockModelRuntimeClient)(nil).LoadModel), varargs...)
}

// ModelSize mocks base method.
func (m *MockModelRuntimeClient) ModelSize(arg0 context.Context, arg1 *mmesh.ModelSizeRequest, arg2 ...grpc.CallOption) (*mmesh.ModelSizeResponse, error) {
	m.ctrl.T.Helper()
	varargs := []interface{}{arg0, arg1}
	for _, a := range arg2 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "ModelSize", varargs...)
	ret0, _ := ret[0].(*mmesh.ModelSizeResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ModelSize indicates an expected call of ModelSize.
func (mr *MockModelRuntimeClientMockRecorder) ModelSize(arg0, arg1 interface{}, arg2 ...interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]interface{}{arg0, arg1}, arg2...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ModelSize", reflect.TypeOf((*MockModelRuntimeClient)(nil).ModelSize), varargs...)
}

// PredictModelSize mocks base method.
func (m *MockModelRuntimeClient) PredictModelSize(arg0 context.Context, arg1 *mmesh.PredictModelSizeRequest, arg2 ...grpc.CallOption) (*mmesh.PredictModelSizeResponse, error) {
	m.ctrl.T.Helper()
	varargs := []interface{}{arg0, arg1}
	for _, a := range arg2 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "PredictModelSize", varargs...)
	ret0, _ := ret[0].(*mmesh.PredictModelSizeResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// PredictModelSize indicates an expected call of PredictModelSize.
func (mr *MockModelRuntimeClientMockRecorder) PredictModelSize(arg0, arg1 interface{}, arg2 ...interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]interface{}{arg0, arg1}, arg2...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PredictModelSize", reflect.TypeOf((*MockModelRuntimeClient)(nil).PredictModelSize), varargs...)
}

// RuntimeStatus mocks base method.
func (m *MockModelRuntimeClient) RuntimeStatus(arg0 context.Context, arg1 *mmesh.RuntimeStatusRequest, arg2 ...grpc.CallOption) (*mmesh.RuntimeStatusResponse, error) {
	m.ctrl.T.Helper()
	varargs := []interface{}{arg0, arg1}
	for _, a := range arg2 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "RuntimeStatus", varargs...)
	ret0, _ := ret[0].(*mmesh.RuntimeStatusResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// RuntimeStatus indicates an expected call of RuntimeStatus.
func (mr *MockModelRuntimeClientMockRecorder) RuntimeStatus(arg0, arg1 interface{}, arg2 ...interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]interface{}{arg0, arg1}, arg2...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RuntimeStatus", reflect.TypeOf((*MockModelRuntimeClient)(nil).RuntimeStatus), varargs...)
}

// UnloadModel mocks base method.
func (m *MockModelRuntimeClient) UnloadModel(arg0 context.Context, arg1 *mmesh.UnloadModelRequest, arg2 ...grpc.CallOption) (*mmesh.UnloadModelResponse, error) {
	m.ctrl.T.Helper()
	varargs := []interface{}{arg0, arg1}
	for _, a := range arg2 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "UnloadModel", varargs...)
	ret0, _ := ret[0].(*mmesh.UnloadModelResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// UnloadModel indicates an expected call of UnloadModel.
func (mr *MockModelRuntimeClientMockRecorder) UnloadModel(arg0, arg1 interface{}, arg2 ...interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]interface{}{arg0, arg1}, arg2...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UnloadModel", reflect.TypeOf((*MockModelRuntimeClient)(nil).UnloadModel), varargs...)
}
