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
	"context"
	"os"
	"time"

	"google.golang.org/grpc/credentials/insecure"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	"google.golang.org/grpc"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// This client is currently not used for unit test, can be used to verify [ mesh -> puller -> adapter -> runtime ]

func main() {
	log := zap.New(zap.UseDevMode(true))
	mmeshClientCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(mmeshClientCtx, "localhost:8033", grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))

	if err != nil {
		log.Error(err, "Failed to connect to MMesh")
		os.Exit(1)
	}
	defer conn.Close()

	c := mmesh.NewModelMeshClient(conn)

	mmeshCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp1, err2 := c.RegisterModel(mmeshCtx, &mmesh.RegisterModelRequest{
		ModelId: "tfmnist",
		ModelInfo: &mmesh.ModelInfo{
			Path: "1/model.savedmodel",
			Type: "tf",
			Key:  "{\"storage_key\":\"myStorage\",\"bucket\":\"triton-models\"}",
		},
		LoadNow: true,
		Sync:    true,
	})

	if err2 != nil {
		log.Error(err2, "Failed to call MMesh")
		os.Exit(1)
	}

	if resp1.Status != mmesh.ModelStatusInfo_LOADED {
		log.Error(nil, "Load model failed", "response_status", resp1.Status)
		os.Exit(1)
	}

	log.Info("runtime status", "response", resp1)

	time.Sleep(30 * time.Second)

	mmeshCtx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp2, err3 := c.GetModelStatus(mmeshCtx, &mmesh.GetStatusRequest{ModelId: "tfmnist"})
	if err3 != nil {
		log.Error(err3, "Failed to call MMesh")
		os.Exit(1)
	}

	log.Info("model status", "response", resp2)

	mmeshCtx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp3, err4 := c.UnregisterModel(mmeshCtx, &mmesh.UnregisterModelRequest{ModelId: "tfmnist"})
	if err4 != nil {
		log.Error(err4, "Failed to call MMesh")
		os.Exit(1)
	}

	log.Info("Unload model status", "response", resp3)

	log.Info("Test finished executing successfully !!!..")
}
