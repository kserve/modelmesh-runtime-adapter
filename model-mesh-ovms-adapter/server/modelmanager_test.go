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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// Make it easy to mock OVMS HTTP responses
type MockOVMS struct {
	server         *httptest.Server
	reloadResponse string
	configResponse string
}

func NewMockOVMS() *MockOVMS {
	m := &MockOVMS{}

	serverMux := http.NewServeMux()

	serverMux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, m.configResponse)
	})

	serverMux.HandleFunc("/config/reload", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println(m.configResponse)
		fmt.Fprintln(w, m.reloadResponse)
	})

	m.server = httptest.NewServer(serverMux)

	return m
}

func (m *MockOVMS) Close() {
	m.server.Close()
}

func (m *MockOVMS) GetAddress() string {
	return m.server.URL
}

func (m *MockOVMS) setMockReloadResponse(c OvmsConfigResponse) error {
	mockResponseBytes, err := json.Marshal(c)
	if err != nil {
		return err
	}
	m.reloadResponse = string(mockResponseBytes)

	return nil
}

func (m *MockOVMS) setMockConfigResponse(c OvmsConfigResponse) error {
	mockResponseBytes, err := json.Marshal(c)
	if err != nil {
		return err
	}
	m.configResponse = string(mockResponseBytes)

	return nil
}

var mockOVMS *MockOVMS

func TestMain(m *testing.M) {
	// create the mock OVMS server
	mockOVMS = NewMockOVMS()
	defer mockOVMS.Close()

	os.Exit(m.Run())
}

func TestHappyPath(t *testing.T) {
	mm := NewOvmsModelManager(mockOVMS.GetAddress(), testModelConfigFile, log)

	mockOVMS.setMockReloadResponse(OvmsConfigResponse{
		testOpenvinoModelId: OvmsModelStatusResponse{
			ModelVersionStatus: []OvmsModelVersionStatus{
				{State: "AVAILABLE"},
			},
		},
	})

	ctx := context.Background()
	err := mm.LoadModel(ctx, filepath.Join(testdataDir, "models", testOpenvinoModelId), testOpenvinoModelId)
	if err != nil {
		t.Errorf("LoadModel call failed: %v", err)
	}
}
