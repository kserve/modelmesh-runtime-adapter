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
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
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

// use TestMain for set-up and tear-down
func TestMain(m *testing.M) {
	// remove the generated testdata dir if it exists to clean up from previous runs,
	// but also ensure it exists for this one
	if _, err := os.Stat(generatedTestdataDir); err == nil {
		if err = os.RemoveAll(generatedTestdataDir); err != nil {
			log.Error(err, "Failed to remove generated dir during test setup")
			os.Exit(1)
		}
	}
	if err := os.MkdirAll(generatedTestdataDir, 0755); err != nil {
		log.Error(err, "Failed to remove generated dir during test setup")
		os.Exit(1)
	}

	// create the mock OVMS server that is shared across tests (maybe it shouldn't be...)
	mockOVMS = NewMockOVMS()
	defer mockOVMS.Close()

	os.Exit(m.Run())
}

func TestAdapter(t *testing.T) {
	// Start the OVMS Adapter
	os.Setenv(ovmsContainerMemReqBytes, fmt.Sprintf("%d", testOvmsContainerMemReqBytes))
	os.Setenv(modelSizeMultiplier, fmt.Sprintf("%f", testModelSizeMultiplier))
	os.Setenv(adapterPort, fmt.Sprintf("%d", testAdapterPort))
	os.Setenv(runtimePort, strings.Split(mockOVMS.GetAddress(), ":")[2])
	os.Setenv(modelConfigFile, testModelConfigFile)
	os.Setenv(rootModelDir, generatedTestdataDir)

	adapterProc, err := StartProcess(*ovmsAdapter)

	if err != nil {
		t.Fatalf("Failed to start to OVMS Adapter:%s, error %v", *ovmsAdapter, err)
	}
	go adapterProc.Wait()
	defer adapterProc.Kill()

	// set mock response to a successful load
	// do this before running RuntimeStatus which calls UnloadAll which triggers a reload
	mockOVMS.setMockReloadResponse(OvmsConfigResponse{
		testOpenvinoModelId: OvmsModelStatusResponse{
			ModelVersionStatus: []OvmsModelVersionStatus{
				{State: "AVAILABLE"},
			},
		},
		testOnnxModelId: OvmsModelStatusResponse{
			ModelVersionStatus: []OvmsModelVersionStatus{
				{State: "AVAILABLE"},
			},
		},
	}, http.StatusOK)

	mmeshClientCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(mmeshClientCtx, fmt.Sprintf("localhost:%d", testAdapterPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
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

	openvinoModelDir := filepath.Join(generatedTestdataDir, ovmsModelSubdir, testOpenvinoModelId)
	openvinoModelFile := filepath.Join(openvinoModelDir, "1", "mapping-config.json")
	if exists, existsErr := util.FileExists(openvinoModelFile); !exists {
		if existsErr != nil {
			t.Errorf("Expected model file %s to exists but got an error checking: %v", openvinoModelFile, existsErr)
		} else {
			t.Errorf("Expected model file %s to exist but it doesn't.", openvinoModelFile)
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

	onnxModelDir := filepath.Join(generatedTestdataDir, ovmsModelSubdir, testOnnxModelId)
	if err = checkEntryExistsInModelConfig(testOnnxModelId, onnxModelDir); err != nil {
		t.Errorf("checkEntryExistsInModelConfig: %v", err)
	}
	// the previously loaded model should also still exist
	if err = checkEntryExistsInModelConfig(testOpenvinoModelId, openvinoModelDir); err != nil {
		t.Errorf("checkEntryExistsInModelConfig: %v", err)
	}

	t.Logf("runtime status: Model loaded, %v", onnxLoadResp)

	// Unload the ONNX Model

	// set the mocked response
	mockOVMS.setMockReloadResponse(OvmsConfigResponse{
		testOpenvinoModelId: OvmsModelStatusResponse{
			ModelVersionStatus: []OvmsModelVersionStatus{
				{State: "AVAILABLE"},
			},
		},
		testOnnxModelId: OvmsModelStatusResponse{
			ModelVersionStatus: []OvmsModelVersionStatus{
				{State: "END"},
			},
		},
	}, http.StatusOK)

	mmeshCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp4, err := c.UnloadModel(mmeshCtx, &mmesh.UnloadModelRequest{
		ModelId: testOnnxModelId,
	})

	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}

	t.Logf("runtime status: Model unloaded, %s", resp4)

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
		return fmt.Errorf("Could not find model '%s' with path '%s' in config '%s'", modelid, path, string(configBytes))
	}

	return nil
}
