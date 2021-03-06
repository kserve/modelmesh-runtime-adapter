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
// Source: puller/puller.go (interfaces: PullerInterface)

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	pullman "github.com/kserve/modelmesh-runtime-adapter/pullman"
)

// MockPullerInterface is a mock of PullerInterface interface.
type MockPullerInterface struct {
	ctrl     *gomock.Controller
	recorder *MockPullerInterfaceMockRecorder
}

// MockPullerInterfaceMockRecorder is the mock recorder for MockPullerInterface.
type MockPullerInterfaceMockRecorder struct {
	mock *MockPullerInterface
}

// NewMockPullerInterface creates a new mock instance.
func NewMockPullerInterface(ctrl *gomock.Controller) *MockPullerInterface {
	mock := &MockPullerInterface{ctrl: ctrl}
	mock.recorder = &MockPullerInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockPullerInterface) EXPECT() *MockPullerInterfaceMockRecorder {
	return m.recorder
}

// Pull mocks base method.
func (m *MockPullerInterface) Pull(arg0 context.Context, arg1 pullman.PullCommand) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Pull", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// Pull indicates an expected call of Pull.
func (mr *MockPullerInterfaceMockRecorder) Pull(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Pull", reflect.TypeOf((*MockPullerInterface)(nil).Pull), arg0, arg1)
}
