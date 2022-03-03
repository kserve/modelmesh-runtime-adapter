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
	"flag"
	"fmt"
	"io/ioutil"
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
const testOvmsContainerMemReqBytes = 6 * 1024 * 1024 * 1024 // 6GB
const testAdapterPort = 8085

var log = zap.New(zap.UseDevMode(true))
var testdataDir = abs("testdata")
var generatedTestdataDir = filepath.Join(testdataDir, "generated")
var ovmsModelsDir = filepath.Join(generatedTestdataDir, ovmsModelSubdir)

const testOnnxModelId = "onnx-mnist"
const testOpenvinoModelId = "openvino-ir"

var testOnnxModelPath = filepath.Join(testdataDir, "models", testOnnxModelId)
var testOpenvinoModelPath = filepath.Join(testdataDir, "models", testOpenvinoModelId)

var testModelConfigFile = filepath.Join(generatedTestdataDir, "model_config_list.json")

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

var ovmsAdapter = flag.String("OvmsAdapter", "../main", "Executable for Openvino Model Server Adapter")

func TestAdapter(t *testing.T) {
	// ensure test dir exists
	os.MkdirAll(generatedTestdataDir, 0755)

	// Start the mock OVMS HTTP server

	//  create a mock model status response to return on all calls
	mockResponse := OvmsConfigResponse{
		// onnx
		testOnnxModelId: OvmsModelStatusResponse{
			ModelVersionStatus: []OvmsModelVersionStatus{
				{State: "AVAILABLE"},
			},
		},

		// openvino_ir
		testOpenvinoModelId: OvmsModelStatusResponse{
			ModelVersionStatus: []OvmsModelVersionStatus{
				{State: "AVAILABLE"},
			},
		},
	}
	mockResponseBytes, err := json.Marshal(mockResponse)
	if err != nil {
		t.Fatalf("Failed to serialize mock JSON: %v", err)
	}

	mockOVMS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, string(mockResponseBytes))
	}))
	defer mockOVMS.Close()

	// Start the OVMS Adapter
	os.Setenv(ovmsContainerMemReqBytes, fmt.Sprintf("%d", testOvmsContainerMemReqBytes))
	os.Setenv(modelSizeMultiplier, fmt.Sprintf("%f", testModelSizeMultiplier))
	os.Setenv(adapterPort, fmt.Sprintf("%d", testAdapterPort))
	os.Setenv(runtimePort, strings.Split(mockOVMS.URL, ":")[2])
	os.Setenv(modelConfigFile, testModelConfigFile)
	os.Setenv(rootModelDir, testdataDir)

	adapterProc, err := StartProcess(*ovmsAdapter)

	if err != nil {
		t.Fatalf("Failed to start to OVMS Adapter:%s, error %v", *ovmsAdapter, err)
	}
	go adapterProc.Wait()
	defer adapterProc.Kill()

	mmeshClientCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(mmeshClientCtx, fmt.Sprintf("localhost:%d", testAdapterPort), grpc.WithBlock(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Failed to connect to MMesh: %v", err)
	}
	defer conn.Close()

	c := mmesh.NewModelRuntimeClient(conn)

	mmeshCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	statusResp, err := c.RuntimeStatus(mmeshCtx, &mmesh.RuntimeStatusRequest{})
	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}
	expectedCapacity := testOvmsContainerMemReqBytes - defaultOvmsMemBufferBytes
	if statusResp.CapacityInBytes != uint64(expectedCapacity) {
		t.Errorf("Expected response's CapacityInBytes to be %d but found %d", expectedCapacity, statusResp.CapacityInBytes)
	}

	t.Logf("runtime status: %v", statusResp)

	mmeshCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	openvinoLoadResp, err := c.LoadModel(mmeshCtx, &mmesh.LoadModelRequest{
		ModelId:   testOpenvinoModelId,
		ModelType: "rt:openvino",
		ModelPath: testOpenvinoModelPath,
		ModelKey:  `{"model_type": "openvino"}`,
	})

	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}
	if openvinoLoadResp.SizeInBytes != defaultModelSizeInBytes {
		t.Errorf("Expected SizeInBytes to be the default %d but actual value was %d", defaultModelSizeInBytes, openvinoLoadResp.SizeInBytes)
	}

	openvinoModelDir := filepath.Join(testdataDir, ovmsModelSubdir, testOpenvinoModelId)
	opnvinoModelFile := filepath.Join(openvinoModelDir, "1", "mapping-config.json")
	if exists, existsErr := util.FileExists(opnvinoModelFile); !exists {
		if existsErr != nil {
			t.Errorf("Expected model file %s to exists but got an error checking: %v", opnvinoModelFile, existsErr)
		} else {
			t.Errorf("Expected model file %s to exist but it doesn't.", opnvinoModelFile)
		}
	}

	if err = checkEntryExistsInModelConfig(testOpenvinoModelId, openvinoModelDir); err != nil {
		t.Errorf("checkEntryExistsInModelConfig: %v", err)
	}

	t.Logf("runtime status: Model loaded, %v", openvinoLoadResp)

	// LoadModel with disk size and model type in model key
	mmeshCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	onnxLoadResp, err := c.LoadModel(mmeshCtx, &mmesh.LoadModelRequest{
		ModelId: testOnnxModelId,
		// direct-to-file model path
		ModelPath: testOnnxModelPath,
		ModelType: "invalid", // this will be ignored
		ModelKey:  `{"storage_key": "myStorage", "bucket": "bucket1", "disk_size_bytes": 54321, "model_type": {"name": "onnx", "version": "x.x"}}`,
	})

	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}
	expectedSizeFloat := 54321 * testModelSizeMultiplier
	expectedSize := uint64(expectedSizeFloat)
	if onnxLoadResp.SizeInBytes != expectedSize {
		t.Errorf("Expected SizeInBytes to be %d but actual value was %d", expectedSize, onnxLoadResp.SizeInBytes)
	}

	onnxModelDir := filepath.Join(testdataDir, ovmsModelSubdir, testOnnxModelId)
	if err = checkEntryExistsInModelConfig(testOnnxModelId, onnxModelDir); err != nil {
		t.Errorf("checkEntryExistsInModelConfig: %v", err)
	}
	// the previously loaded model should also still exist
	if err = checkEntryExistsInModelConfig(testOpenvinoModelId, openvinoModelDir); err != nil {
		t.Errorf("checkEntryExistsInModelConfig: %v", err)
	}

	t.Logf("runtime status: Model loaded, %v", onnxLoadResp)

	// UnloadModel
	mmeshCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp4, err := c.UnloadModel(mmeshCtx, &mmesh.UnloadModelRequest{
		ModelId: testOnnxModelId,
	})

	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}

	t.Logf("runtime status: Model unloaded, %v", resp4)

	// the previously loaded model should also still exist
	if err := checkEntryExistsInModelConfig(testOpenvinoModelId, openvinoModelDir); err != nil {
		t.Errorf("checkEntryExistsInModelConfig: %v", err)
	}
}

func checkEntryExistsInModelConfig(modelid string, path string) error {
	configBytes, err := ioutil.ReadFile(testModelConfigFile)
	if err != nil {
		return fmt.Errorf("Unable to read config file: %w", err)
	}

	var config OvmsMultiModelRepositoryConfig
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return fmt.Errorf("Unable to read config file: %w", err)
	}

	entryFound := false
	for _, entry := range config.ModelConfigList {
		if entry.Config.Name == modelid &&
			entry.Config.BasePath == path {
			entryFound = true
			break
		}
	}

	if !entryFound {
		return fmt.Errorf("Could not find model '%s' in config '%s'", modelid, string(configBytes))
	}

	return nil
}
