// Copyright 2021 IBM Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type mockPullerServer struct {
	loaded   int
	unloaded int
}

func (m *mockPullerServer) loadModel(ctx context.Context, req *mmesh.LoadModelRequest) (*mmesh.LoadModelResponse, error) {
	m.loaded = m.loaded + 1
	return &mmesh.LoadModelResponse{}, nil
}

func (m *mockPullerServer) unloadModel(ctx context.Context, req *mmesh.UnloadModelRequest) (*mmesh.UnloadModelResponse, error) {
	m.unloaded = m.unloaded + 1
	return &mmesh.UnloadModelResponse{}, nil
}

func TestStateManagerLoadModel(t *testing.T) {
	log := zap.New()
	s := NewPullerServer(log)
	sm := s.sm
	mockPullerServer := &mockPullerServer{}
	sm.s = mockPullerServer

	load_req := &mmesh.LoadModelRequest{ModelId: "model-id"}
	sm.loadModel(context.Background(), load_req)

	if mockPullerServer.loaded != 1 {
		t.Fatal("Load should have been called 1 time")
	}
	if mockPullerServer.unloaded != 0 {
		t.Fatal("Unload should not have been called")
	}
	if len(sm.data) > 0 {
		t.Fatal("StateManager map should be empty")
	}

	// now unload the model
	unload_req := &mmesh.UnloadModelRequest{ModelId: "model-id"}
	sm.unloadModel(context.Background(), unload_req)

	if mockPullerServer.unloaded != 1 {
		t.Fatal("Unload should now have been called")
	}
	if len(sm.data) > 0 {
		t.Fatal("StateManager map should be empty")
	}
}

type mockPullerServerError struct {
}

func (m *mockPullerServerError) loadModel(ctx context.Context, req *mmesh.LoadModelRequest) (*mmesh.LoadModelResponse, error) {
	// sleep to simulate a delay that could cause the context to be cancelled
	time.Sleep(50 * time.Millisecond)
	return nil, errors.New("failed load")
}

func (m *mockPullerServerError) unloadModel(ctx context.Context, req *mmesh.UnloadModelRequest) (*mmesh.UnloadModelResponse, error) {
	return nil, errors.New("failed unload")
}

func TestStateManagerErrors(t *testing.T) {
	log := zap.New()
	s := NewPullerServer(log)
	sm := s.sm
	mockPullerServerError := &mockPullerServerError{}
	sm.s = mockPullerServerError

	// check that error returns are handled
	var err error
	load_req := &mmesh.LoadModelRequest{ModelId: "model-id"}
	_, err = sm.loadModel(context.Background(), load_req)
	if err == nil {
		t.Fatal("An error should have been returned")
	}

	unload_req := &mmesh.UnloadModelRequest{ModelId: "model-id"}
	_, err = sm.unloadModel(context.Background(), unload_req)
	if err == nil {
		t.Fatal("An error should have been returned")
	}

	// check that context cancellation is handled
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	_, err = sm.loadModel(ctx, load_req)
	if err == nil {
		t.Fatal("An error should have been returned")
	}

}
