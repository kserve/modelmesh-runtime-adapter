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
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
	"google.golang.org/grpc"
)

const testModelSizeMultiplier = 1.35
const testOpenvinoContainerMemReqBytes = 6 * 1024 * 1024 * 1024 // 6GB
const testAdapterPort = 8085

var log = zap.New(zap.UseDevMode(true))
var testdataDir = abs("testdata")
var generatedTestdataDir = filepath.Join(testdataDir, "generated")
var openvinoModelsDir = filepath.Join(generatedTestdataDir, openvinoModelSubdir)

var testModelConfigFile = filepath.Join(testdataDir, "model_config_list.json")

func abs(path string) string {
	a, err := filepath.Abs(path)
	if err != nil {
		panic("Could not get absolute path of " + path + " " + err.Error())
	}
	return a
}

func StartProcess(args ...string) (p *os.Process, err error) {
	if args[0], err = exec.LookPath(args[0]); err == nil {
		var procAttr os.ProcAttr
		procAttr.Files = []*os.File{os.Stdin,
			os.Stdout, os.Stderr}
		p, err = os.StartProcess(args[0], args, &procAttr)
		if err == nil {
			return p, nil
		}
	}
	return nil, err
}

var openvinoAdapter = flag.String("OpenvinoAdapter", "../main", "Executable for Openvino Adapter")

func TestAdapter(t *testing.T) {
	// Start the mock OVMS HTTP server
	mockOVMS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "{}")
	}))
	defer mockOVMS.Close()

	// Start the OVMS Adapter
	os.Setenv(openvinoContainerMemReqBytes, fmt.Sprintf("%d", testOpenvinoContainerMemReqBytes))
	os.Setenv(modelSizeMultiplier, fmt.Sprintf("%f", testModelSizeMultiplier))
	os.Setenv(adapterPort, fmt.Sprintf("%d", testAdapterPort))
	os.Setenv(runtimePort, strings.Split(mockOVMS.URL, ":")[2])
	os.Setenv(modelConfigFile, testModelConfigFile)
	os.Setenv(rootModelDir, testdataDir)

	adapterProc, err := StartProcess(*openvinoAdapter)

	if err != nil {
		t.Fatalf("Failed to start to Openvino Adapter:%s, error %v", *openvinoAdapter, err)
	}
	go adapterProc.Wait()
	defer adapterProc.Kill()

	time.Sleep(5 * time.Second)

	mmeshClientCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(mmeshClientCtx, fmt.Sprintf("localhost:%d", testAdapterPort), grpc.WithBlock(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Failed to connect to MMesh: %v", err)
	}
	defer conn.Close()

	c := mmesh.NewModelRuntimeClient(conn)

	mmeshCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp1, err := c.RuntimeStatus(mmeshCtx, &mmesh.RuntimeStatusRequest{})
	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}
	expectedCapacity := testOpenvinoContainerMemReqBytes - defaultOpenvinoMemBufferBytes
	if resp1.CapacityInBytes != uint64(expectedCapacity) {
		t.Errorf("Expected response's CapacityInBytes to be %d but found %d", expectedCapacity, resp1.CapacityInBytes)
	}

	t.Logf("runtime status: %v", resp1)

	mmeshCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp2, err := c.LoadModel(mmeshCtx, &mmesh.LoadModelRequest{
		ModelId:   "tfmnist",
		ModelType: "rt:openvino",
		ModelPath: filepath.Join(testdataDir, "tfmnist"),
		ModelKey:  `{"model_type": "onnx"}`,
	})

	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}
	if resp2.SizeInBytes != defaultModelSizeInBytes {
		t.Errorf("Expected SizeInBytes to be the default %d but actual value was %d", defaultModelSizeInBytes, resp2.SizeInBytes)
	}

	modelDir := filepath.Join(testdataDir, openvinoModelSubdir, "tfmnist", "1", "model.onnx")
	if exists, existsErr := util.FileExists(modelDir); !exists {
		if existsErr != nil {
			t.Errorf("Expected model dir %s to exists but got an error checking: %v", modelDir, existsErr)
		} else {
			t.Errorf("Expected model dir %s to exist but it doesn't.", modelDir)
		}
	}

	t.Logf("runtime status: Model loaded, %v", resp2)

	// LoadModel with disk size and model type in model key
	mmeshCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp3, err := c.LoadModel(mmeshCtx, &mmesh.LoadModelRequest{
		ModelId:   "tfmnist",
		ModelPath: filepath.Join(testdataDir, "tfmnist"),
		ModelType: "invalid", // this will be ignored
		ModelKey:  `{"storage_key": "myStorage", "bucket": "bucket1", "disk_size_bytes": 54321, "model_type": {"name": "tensorflow", "version": "1.5"}}`,
	})

	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}
	expectedSizeFloat := 54321 * testModelSizeMultiplier
	expectedSize := uint64(expectedSizeFloat)
	if resp3.SizeInBytes != expectedSize {
		t.Errorf("Expected SizeInBytes to be %d but actual value was %d", expectedSize, resp3.SizeInBytes)
	}

	t.Logf("runtime status: Model loaded, %v", resp3)

	// UnloadModel
	mmeshCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp4, err := c.UnloadModel(mmeshCtx, &mmesh.UnloadModelRequest{
		ModelId: "tfmnist",
	})

	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}

	t.Logf("runtime status: Model unloaded, %v", resp4)

	modelDir = filepath.Join(testdataDir, openvinoModelSubdir, "tfmnist")
	exists, err := util.FileExists(modelDir)
	if err != nil {
		t.Errorf("Expected model dir %s to not exist but got an error checking: %v", modelDir, err)
	} else if exists {
		t.Errorf("Expected model dir %s to not exist but it does.", modelDir)
	}
}
