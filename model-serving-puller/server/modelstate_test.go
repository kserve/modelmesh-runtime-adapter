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
	"testing"

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

	req := &mmesh.LoadModelRequest{ModelId: "model-id"}
	sm.loadModel(context.Background(), req)

	if mockPullerServer.loaded != 1 {
		t.Fatal("Load should have been called 1 time")
	}
	if mockPullerServer.unloaded != 0 {
		t.Fatal("Load should have been called 1 time")
	}
	if len(sm.data) > 0 {
		t.Fatal("StateManager map should be empty")
	}
}
