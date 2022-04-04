// Copyright 2022 IBM Corporation
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
	"path/filepath"
	"strings"
	"testing"
)

// Make it easy to mock OVMS HTTP responses
type MockOVMS struct {
	server             *httptest.Server
	reloadResponse     string
	reloadResponseCode int
	configResponse     string
	configResponseCode int
}

func NewMockOVMS() *MockOVMS {
	m := &MockOVMS{
		configResponse:     "{}",
		configResponseCode: http.StatusOK,
		reloadResponse:     "{}",
		reloadResponseCode: http.StatusOK,
	}

	serverMux := http.NewServeMux()

	serverMux.HandleFunc("/v1/config", func(w http.ResponseWriter, r *http.Request) {
		if m.configResponseCode == http.StatusOK {
			fmt.Fprintln(w, m.configResponse)
		} else {
			http.Error(w, m.configResponse, m.configResponseCode)
		}
	})

	serverMux.HandleFunc("/v1/config/reload", func(w http.ResponseWriter, r *http.Request) {
		if m.reloadResponseCode == http.StatusOK {
			fmt.Fprintln(w, m.reloadResponse)
		} else {
			http.Error(w, m.reloadResponse, m.reloadResponseCode)
		}
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

func (m *MockOVMS) setMockReloadResponse(c interface{}, code int) error {
	mockResponseBytes, err := json.Marshal(c)
	if err != nil {
		return err
	}
	m.reloadResponse = string(mockResponseBytes)
	m.reloadResponseCode = code

	return nil
}

func (m *MockOVMS) setMockConfigResponse(c OvmsConfigResponse, code int) error {
	mockResponseBytes, err := json.Marshal(c)
	if err != nil {
		return err
	}
	m.configResponse = string(mockResponseBytes)
	m.configResponseCode = code

	return nil
}

// shared instance of the mock server
var mockOVMS *MockOVMS

func setupModelManager(t *testing.T) *OvmsModelManager {
	mm, err := NewOvmsModelManager(mockOVMS.GetAddress(), testModelConfigFile, log, ModelManagerConfig{})
	if err != nil {
		t.Fatalf("Unable to create ModelManager with Mock: %v", err)
	}
	return mm
}

func TestHappyPathLoadAndUnload(t *testing.T) {
	mm := setupModelManager(t)

	mockOVMS.setMockReloadResponse(OvmsConfigResponse{
		testOpenvinoModelId: OvmsModelStatusResponse{
			ModelVersionStatus: []OvmsModelVersionStatus{
				{State: "AVAILABLE"},
			},
		},
	}, http.StatusOK)

	ctx := context.Background()
	if err := mm.LoadModel(ctx, filepath.Join(testdataDir, "models", testOpenvinoModelId), testOpenvinoModelId); err != nil {
		t.Errorf("LoadModel call failed: %v", err)
	}

	if err := checkEntryExistsInModelConfig(testOpenvinoModelId, testOpenvinoModelPath); err != nil {
		t.Error(err)
	}

	// update mock response for the unload
	mockOVMS.setMockReloadResponse(OvmsConfigResponse{
		testOpenvinoModelId: OvmsModelStatusResponse{
			ModelVersionStatus: []OvmsModelVersionStatus{
				{State: "END"},
			},
		},
	}, http.StatusOK)

	if err := mm.UnloadModel(ctx, testOpenvinoModelId); err != nil {
		t.Errorf("UnloadModel call failed: %v", err)
	}
}

func TestLoadFailure(t *testing.T) {
	mm := setupModelManager(t)

	mockOVMS.setMockReloadResponse(OvmsConfigErrorResponse{Error: "Reloading models versions failed"}, http.StatusBadRequest)

	expectedErrorMessage := "Test model load failure"
	mockOVMS.setMockConfigResponse(OvmsConfigResponse{
		testOpenvinoModelId: OvmsModelStatusResponse{
			ModelVersionStatus: []OvmsModelVersionStatus{
				{
					State: "LOADING",
					Status: OvmsModelStatus{
						ErrorMessage: expectedErrorMessage,
					},
				},
			},
		},
	}, http.StatusOK)

	ctx := context.Background()

	err := mm.LoadModel(ctx, filepath.Join(testdataDir, "models", testOpenvinoModelId), testOpenvinoModelId)

	if err == nil {
		t.Errorf("Model should have failed to load")
	}

	if !strings.Contains(err.Error(), expectedErrorMessage) {
		t.Errorf("Model load failed with unexpected error string: %s", err.Error())
	}

}
