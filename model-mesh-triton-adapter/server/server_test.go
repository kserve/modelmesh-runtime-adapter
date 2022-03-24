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
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc/credentials/insecure"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	triton "github.com/kserve/modelmesh-runtime-adapter/internal/proto/triton"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/prototext"
)

const testModelSizeMultiplier = 1.35
const testTritonContainerMemReqBytes = 6 * 1024 * 1024 * 1024 // 6GB

var log = zap.New(zap.UseDevMode(true))
var testdataDir = abs("testdata")
var generatedTestdataDir = filepath.Join(testdataDir, "generated")
var tritonModelsDir string = filepath.Join(generatedTestdataDir, tritonModelSubdir)

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

var mockTriton = flag.String("TritonServer", "../triton/mocktriton", "Executable for Triton Server")
var tritonAdapter = flag.String("TritonAdapter", "../main", "Executable for Triton Adapter")

func TestAdapter(t *testing.T) {
	os.Setenv(tritonContainerMemReqBytes, fmt.Sprintf("%d", testTritonContainerMemReqBytes))
	os.Setenv(modelSizeMultiplier, fmt.Sprintf("%f", testModelSizeMultiplier))
	os.Setenv(rootModelDir, testdataDir)
	mockTritonProc, err := StartProcess(*mockTriton)

	if err != nil {
		t.Fatalf("Failed to start to Mock Triton Server:%s, error %v", *mockTriton, err)
	}

	go mockTritonProc.Wait()
	defer mockTritonProc.Kill()

	time.Sleep(5 * time.Second)

	adapterProc, err := StartProcess(*tritonAdapter)

	if err != nil {
		t.Fatalf("Failed to start to Triton Adapter:%s, error %v", *tritonAdapter, err)
	}
	go adapterProc.Wait()
	defer adapterProc.Kill()

	time.Sleep(5 * time.Second)

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
	expectedCapacity := testTritonContainerMemReqBytes - defaultTritonMemBufferBytes
	if resp1.CapacityInBytes != uint64(expectedCapacity) {
		t.Errorf("Expected response's CapacityInBytes to be %d but found %d", expectedCapacity, resp1.CapacityInBytes)
	}

	t.Logf("runtime status: %v", resp1)

	mmeshCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp2, err := c.LoadModel(mmeshCtx, &mmesh.LoadModelRequest{
		ModelId:   "tfmnist",
		ModelType: "TensorFlow",
		ModelPath: filepath.Join(testdataDir, "tfmnist"),
		ModelKey:  "{}",
	})

	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}
	if resp2.SizeInBytes != defaultModelSizeInBytes {
		t.Errorf("Expected SizeInBytes to be the default %d but actual value was %d", defaultModelSizeInBytes, resp2.SizeInBytes)
	}

	modelDir := filepath.Join(testdataDir, tritonModelSubdir, "tfmnist", "1", "model.savedmodel")
	if exists, existsErr := util.FileExists(modelDir); !exists {
		if existsErr != nil {
			t.Errorf("Expected model dir %s to exists but got an error checking: %v", modelDir, existsErr)
		} else {
			t.Errorf("Expected model dir %s to exist but it doesn't.", modelDir)
		}
	}

	configFile := filepath.Join(testdataDir, tritonModelSubdir, "tfmnist", "config.pbtxt")
	if exists, existsErr := util.FileExists(configFile); !exists {
		if existsErr != nil {
			t.Errorf("Expected config file %s to exist but got an error checking: %v", modelDir, existsErr)
		} else {
			t.Errorf("Expected config file %s to exist but it doesn't.", modelDir)
		}
	}

	// Check that the `name` property was removed from the config file
	{
		var err1 error
		pbtxt, err1 := ioutil.ReadFile(configFile)
		if err1 != nil {
			t.Errorf("Unable to read config file %s: %v", configFile, err)
		}
		m := triton.ModelConfig{}
		prototext.Unmarshal(pbtxt, &m)

		t.Logf("%v", m.Name)
		if m.Name != "" {
			t.Errorf("Expected `name` parameter to be stripped from config file %s but got value of [%s]", configFile, m.Name)
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

	modelDir = filepath.Join(testdataDir, tritonModelSubdir, "tfmnist")
	exists, err := util.FileExists(modelDir)
	if err != nil {
		t.Errorf("Expected model dir %s to not exist but got an error checking: %v", modelDir, err)
	} else if exists {
		t.Errorf("Expected model dir %s to not exist but it does.", modelDir)
	}
}
