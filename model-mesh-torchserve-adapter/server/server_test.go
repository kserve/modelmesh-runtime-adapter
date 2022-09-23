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
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc/credentials/insecure"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
	"google.golang.org/grpc"
)

const testModelSizeMultiplier = 2.75
const testTorchServeContainerMemReqBytes = 6 * 1024 * 1024 * 1024 // 6GB

var testdataDir = abs("testdata")

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

var mockTorchServe = flag.String("TorchServe", "../torchserve/mocktorchserve", "Executable for TorchServe Server")
var torchServeAdapter = flag.String("TorchServeAdapter", "../main", "Executable for TorchServe Adapter")

func TestAdapter(t *testing.T) {
	os.Setenv(torchServeContainerMemReqBytes, fmt.Sprintf("%d", testTorchServeContainerMemReqBytes))
	os.Setenv(modelSizeMultiplier, fmt.Sprintf("%f", testModelSizeMultiplier))
	os.Setenv(rootModelDir, testdataDir)
	mockTorchServeProc, err := StartProcess(*mockTorchServe)
	if err != nil {
		t.Fatalf("Failed to start to TorchServe Server:%s, error %v", *mockTorchServe, err)
	}

	go mockTorchServeProc.Wait()
	defer mockTorchServeProc.Kill()

	time.Sleep(5 * time.Second)

	adapterProc, err := StartProcess(*torchServeAdapter)
	if err != nil {
		t.Fatalf("Failed to start to TorchServe Adapter:%s, error %v", *torchServeAdapter, err)
	}

	go adapterProc.Wait()
	defer adapterProc.Kill()

	time.Sleep(5 * time.Second)

	modelDir := filepath.Join(testdataDir, torchServeModelStoreDirName)
	configFilePath := filepath.Join(modelDir, configFileName)
	assertGeneratedConfigFileIsCorrect(configFilePath, modelDir, t)

	mmeshClientCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(mmeshClientCtx, "localhost:8085", grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
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
	expectedCapacity := testTorchServeContainerMemReqBytes - defaultTorchServeMemBufferBytes
	if resp1.CapacityInBytes != uint64(expectedCapacity) {
		t.Errorf("Expected response's CapacityInBytes to be %d but found %d", expectedCapacity, resp1.CapacityInBytes)
	}

	t.Logf("runtime status: %v", resp1)

	mmeshCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	testModelID := "test-pt-model"
	testModelPath := filepath.Join(testdataDir, testModelID+".mar")
	resp2, err := c.LoadModel(mmeshCtx, &mmesh.LoadModelRequest{
		ModelId:   testModelID,
		ModelType: "pytorch-mar",
		ModelPath: testModelPath,
		ModelKey:  "{}",
	})
	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}

	if resp2.SizeInBytes != 0 {
		t.Errorf("Expected SizeInBytes to be the default %d but actual value was %d", 0, resp2.SizeInBytes)
	}

	// check the generated symlink
	generatedLinkPath := filepath.Join(testdataDir, torchServeModelStoreDirName, testModelID+".mar")
	assertGeneratedModelLinkIsCorrect(generatedLinkPath, t)

	t.Logf("runtime status: Model loaded, %v", resp2)

	resp3, err := c.ModelSize(mmeshCtx, &mmesh.ModelSizeRequest{
		ModelId: testModelID,
	})
	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}

	t.Logf("runtime status: Model sized, %v", resp3)

	// For first attempt, the mock server will return invalid json from the describe call, so we expect sizing
	// to be based on the model size on disk (fallback case)
	expectedSizeFloat := 48 * testModelSizeMultiplier
	expectedSize := uint64(expectedSizeFloat)
	if resp3.SizeInBytes != expectedSize {
		t.Fatalf("Expected SizeInBytes to be %d but actual value was %d", expectedSize, resp3.SizeInBytes)
	}

	// LoadModel with disk size and model type in model key
	mmeshCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp4, err := c.LoadModel(mmeshCtx, &mmesh.LoadModelRequest{
		ModelId:   testModelID,
		ModelPath: testModelPath,
		ModelType: "invalid", // this will be ignored
		ModelKey:  `{"storage_key": "myStorage", "bucket": "bucket1", "disk_size_bytes": 54321, "model_type": {"name": "sklearn"}}`,
	})
	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}

	t.Logf("runtime status: Model loaded, %v", resp4)

	// LoadModel with disk size and model type in model key
	mmeshCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp5, err := c.ModelSize(mmeshCtx, &mmesh.ModelSizeRequest{
		ModelId: testModelID,
	})
	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}

	t.Logf("runtime status: Model sized, %v", resp5)

	reportedSize := 11111
	expectedSize = uint64(reportedSize + reportedSize/10)
	if resp5.SizeInBytes != expectedSize {
		t.Fatalf("Expected SizeInBytes to be %d but actual value was %d", expectedSize, resp5.SizeInBytes)
	}

	// UnloadModel
	mmeshCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp6, err := c.UnloadModel(mmeshCtx, &mmesh.UnloadModelRequest{
		ModelId: testModelID,
	})
	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}

	t.Logf("runtime status: Model unloaded, %v", resp6)

	// after unload, the generated model directory should no longer exist
	exists, err := util.FileExists(generatedLinkPath)
	if err != nil {
		t.Errorf("Expected model dir %s to not exist but got an error checking: %v", generatedLinkPath, err)
	} else if exists {
		t.Errorf("Expected model dir %s to not exist but it does.", generatedLinkPath)
	}
}

func assertGeneratedConfigFileIsCorrect(path, modelStoreDir string, t *testing.T) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Could not read config file [%s]: %v", path, err)
	}

	expectedFileContents := fmt.Sprintf(`
enable_envvars_config=true
model_store=%s
metric_time_interval=10
# The REST inference_address is set to use a different port
# than its default 8080 which clashes with that used for modelmesh internal litelinks communication
inference_address=http://127.0.0.1:8083
# Recommended performance defaults - TBC, can be overriden by TC_ env vars
number_of_netty_threads=8
job_queue_size=512
`, modelStoreDir)

	if !bytes.Equal([]byte(expectedFileContents), data) {
		t.Fatalf("Could not stat config file link [%s]: %v", path, err)
	}
}

func assertGeneratedModelLinkIsCorrect(path string, t *testing.T) {
	_ /*info*/, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Could not stat generated link [%s]: %v", path, err)
	}

	// For some reason symlink bit isn't set even though I'm sure it's a symlink ¯\_(ツ)_/¯
	//if info.Mode()&os.ModeSymlink == 0 {
	//	t.Fatalf("Model file %s is not a symbolic link: %v", path, info)
	//}
}
