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

// GoMock based mock storage provider for testing
// The StorageProvider implementation is "real", but the client is a mock. The
// key identifier for a client can be set in the configuration so as to re-use
// an existing client.
package pullman

import (
	"context"
	"errors"
	reflect "reflect"

	logr "github.com/go-logr/logr"
	gomock "github.com/golang/mock/gomock"
)

const (
	mockConfigKey   = "_clientKey"
	mockConfigError = "_error"
)

type MockStorageProvider struct {
	mockClientMap map[string]*MockRepositoryClient
}

func NewMockStorageProvider() *MockStorageProvider {
	msp := MockStorageProvider{
		mockClientMap: make(map[string]*MockRepositoryClient),
	}
	return &msp
}

func (m *MockStorageProvider) GetKey(config Config) string {
	// allow setting the key in the config, but just return "" if unset
	key, _ := GetString(config, mockConfigKey)
	return key
}

func (m *MockStorageProvider) NewRepository(config Config, log logr.Logger) (RepositoryClient, error) {
	key, _ := GetString(config, mockConfigKey)
	mc, ok := m.mockClientMap[key]
	if !ok {
		return nil, errors.New("must register a client by calling RegisterMockClient first")
	}

	// allow an error to be registered and returned for testing error
	// handling
	err, ok := config.Get(mockConfigError)
	if ok {
		return mc, err.(error)
	}
	return mc, nil
}

func (m *MockStorageProvider) RegisterMockClient(key string, c *MockRepositoryClient) {
	m.mockClientMap[key] = c
}

// The rest of the file is generated code. DO NOT EDIT.

// MockRepositoryClient is a mock of RepositoryClient interface.
type MockRepositoryClient struct {
	ctrl     *gomock.Controller
	recorder *MockRepositoryClientMockRecorder
}

// MockRepositoryClientMockRecorder is the mock recorder for MockRepositoryClient.
type MockRepositoryClientMockRecorder struct {
	mock *MockRepositoryClient
}

// NewMockRepositoryClient creates a new mock instance.
func NewMockRepositoryClient(ctrl *gomock.Controller) *MockRepositoryClient {
	mock := &MockRepositoryClient{ctrl: ctrl}
	mock.recorder = &MockRepositoryClientMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRepositoryClient) EXPECT() *MockRepositoryClientMockRecorder {
	return m.recorder
}

// Pull mocks base method.
// Pull mocks base method.
func (m *MockRepositoryClient) Pull(arg0 context.Context, arg1 PullCommand) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Pull", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// Pull indicates an expected call of Pull.
func (mr *MockRepositoryClientMockRecorder) Pull(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Pull", reflect.TypeOf((*MockRepositoryClient)(nil).Pull), arg0, arg1)
}
