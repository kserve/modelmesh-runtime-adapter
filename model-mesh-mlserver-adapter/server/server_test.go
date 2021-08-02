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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	"google.golang.org/grpc"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const testModelSizeMultiplier = 1.35
const testMLServerContainerMemReqBytes = 6 * 1024 * 1024 * 1024 // 6GB

var log = zap.New(zap.UseDevMode(true))
var testdataDir = abs("testdata")
var generatedTestdataDir = filepath.Join(testdataDir, "generated")
var generatedMlserverModelsDir string = filepath.Join(generatedTestdataDir, mlserverModelSubdir)

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

var mockMLServer = flag.String("MLServer", "../mlserver/mockmlserver", "Executable for MLServer Server")
var mlserverAdapter = flag.String("MLServerAdapter", "../main", "Executable for MLServer Adapter")

func TestAdapter(t *testing.T) {
	os.Setenv(mlserverContainerMemReqBytes, fmt.Sprintf("%d", testMLServerContainerMemReqBytes))
	os.Setenv(modelSizeMultiplier, fmt.Sprintf("%f", testModelSizeMultiplier))
	os.Setenv(rootModelDir, testdataDir)
	mockMLServerProc, err := StartProcess(*mockMLServer)

	if err != nil {
		t.Fatalf("Failed to start to Mock MLServer Server:%s, error %v", *mockMLServer, err)
	}

	go mockMLServerProc.Wait()
	defer mockMLServerProc.Kill()

	time.Sleep(5 * time.Second)

	adapterProc, err := StartProcess(*mlserverAdapter)

	if err != nil {
		t.Fatalf("Failed to start to MLServer Adapter:%s, error %v", *mlserverAdapter, err)
	}
	go adapterProc.Wait()
	defer adapterProc.Kill()

	time.Sleep(5 * time.Second)

	mmeshClientCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(mmeshClientCtx, "localhost:8085", grpc.WithBlock(), grpc.WithInsecure())
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
	expectedCapacity := testMLServerContainerMemReqBytes - defaultMLServerMemBufferBytes
	if resp1.CapacityInBytes != uint64(expectedCapacity) {
		t.Errorf("Expected response's CapacityInBytes to be %d but found %d", expectedCapacity, resp1.CapacityInBytes)
	}

	t.Logf("runtime status: %v", resp1)

	mmeshCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	testModelID := "mnist-svm-00000000"
	resp2, err := c.LoadModel(mmeshCtx, &mmesh.LoadModelRequest{
		ModelId:   testModelID,
		ModelType: "sklearn",
		ModelPath: "empty",
	})

	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}
	if resp2.SizeInBytes != defaultModelSizeInBytes {
		t.Errorf("Expected SizeInBytes to be the default %d but actual value was %d", defaultModelSizeInBytes, resp2.SizeInBytes)
	}

	// check the contents of the generated model dir
	sourceModelDir := filepath.Join(testdataDir, testModelID)
	generatedModelDir := filepath.Join(testdataDir, mlserverModelSubdir, testModelID)
	assertGeneratedModelDirIsCorrect(sourceModelDir, generatedModelDir, testModelID, t)

	t.Logf("runtime status: Model loaded, %v", resp2)

	// LoadModel with disk size and model type in model key
	mmeshCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp3, err := c.LoadModel(mmeshCtx, &mmesh.LoadModelRequest{
		ModelId:   testModelID,
		ModelPath: "empty",
		ModelType: "invalid", // this will be ignored
		ModelKey:  `{"storage_key": "myStorage", "bucket": "bucket1", "disk_size_bytes": 54321, "model_type": {"name": "sklearn"}}`,
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
		ModelId: testModelID,
	})

	if err != nil {
		t.Fatalf("Failed to call MMesh: %v", err)
	}

	t.Logf("runtime status: Model unloaded, %v", resp4)

	// after unload, the generated model directory should no longer exist
	exists, err := fileExists(generatedModelDir)
	if err != nil {
		t.Errorf("Expected model dir %s to not exist but got an error checking: %v", generatedModelDir, err)
	} else if exists {
		t.Errorf("Expected model dir %s to not exist but it does.", generatedModelDir)
	}
}

func TestProcessConfigJSON(t *testing.T) {
	jsonIn := `{
    "name": "mnist-svm",
    "implementation": "mlserver_sklearn.SKLearnModel",
    "versions": ["v0.1.0", "v0.2.0"],
    "parameters": {
		"uri": "./mnist-svm-00000000",
        "version": "v0.1.0"
    }
}`

	modelID := "test-model-id"
	json := []byte(jsonIn)
	targetPath := "/targetDir"
	json = processConfigJSON(json, modelID, targetPath, log)

	jsonOut := string(json)
	if strings.Contains(jsonOut, "\"name\": \"mnist-svm\"") || !strings.Contains(jsonOut, "test-model-id") {
		t.Errorf("Expected name field to be replaced in config JSON")
	}
	if !strings.Contains(jsonOut, "\"uri\": \"/targetDir/mnist-svm-00000000\"") {
		t.Errorf("Expected uri field to be replaced in config JSON")
	}

}

func assertGeneratedModelDirIsCorrect(sourceDir string, generatedDir string, modelID string, t *testing.T) {
	generatedFiles, err := ioutil.ReadDir(generatedDir)
	if err != nil {
		t.Errorf("Could not read files in generated dir [%s]: %v", generatedDir, err)
	}

	// config file must exist in the generated dir, so we track if we found it
	configFileFound := false
	for _, f := range generatedFiles {
		filePath := filepath.Join(generatedDir, f.Name())
		if f.Name() == mlserverRepositoryConfigFilename {
			configFileFound = true
			// should have `name` field matching the modelID
			configJSON, err := ioutil.ReadFile(filePath)
			if err != nil {
				t.Errorf("Unable to read config file %s: %v", filePath, err)
			}

			var j map[string]interface{}
			if err := json.Unmarshal(configJSON, &j); err != nil {
				t.Errorf("Unable to unmarshal config file: %v", err)
			}

			if j["name"] != modelID {
				t.Errorf("Expected `name` parameter to be [%s] but got value of [%s]", modelID, j["name"])
			}
			continue
		}

		// if not the config file, it should be a symlink pointing to a file in the
		// source dir that exists
		if f.Mode()&os.ModeSymlink != os.ModeSymlink {
			t.Errorf("Expected [%s] to be a symlink.", filePath)
		}

		resolvedPath, err := filepath.EvalSymlinks(filePath)
		if err != nil {
			t.Errorf("Error resolving symlink [%s]: %v", filePath, err)
		}
		// assert that the target file exists
		if exists, err := fileExists(resolvedPath); !exists {
			if err != nil {
				t.Errorf("Expected file %s to exist but got an error checking: %v", resolvedPath, err)
			} else {
				t.Errorf("Expected file %s to exist but it was not found", resolvedPath)
			}
		}
	}

	if !configFileFound {
		t.Errorf("Did not find config file [%s] in [%s]", mlserverRepositoryConfigFilename, generatedDir)
	}
}

func fileExists(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return true, err
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, err
	}
}

type linkExpectation struct {
	Name   string
	Target string
}

type rewriteModelDirTestCase struct {
	ModelID       string
	ModelType     string
	InputFile     string
	InputFiles    []string
	ExpectedLinks []linkExpectation
}

var rewriteModelDirTests = []rewriteModelDirTestCase{
	// standard native
	{
		ModelID:   "sklearn-standard-native",
		ModelType: "sklearn",
		InputFiles: []string{
			"model-settings.json",
			"model.joblib",
		},
		ExpectedLinks: []linkExpectation{
			{
				Name: "model.joblib",
			},
		},
	},

	// to directory with single file with arbitrary name
	{
		ModelID:   "sklearn-single-file",
		ModelType: "sklearn",
		InputFiles: []string{
			"arbitrary-named-joblib",
		},
		ExpectedLinks: []linkExpectation{
			{
				Name:   "arbitrary-named-joblib",
				Target: "arbitrary-named-joblib",
			},
		},
	},

	// direct to single file with arbitrary name
	{
		ModelID:   "sklearn-direct-to-file",
		ModelType: "sklearn",
		InputFile: "arbitrary-named-joblib",
		ExpectedLinks: []linkExpectation{
			{
				Name:   "arbitrary-named-joblib",
				Target: "arbitrary-named-joblib",
			},
		},
	},

	// standard native
	{
		ModelID:   "xgboost-standard-native",
		ModelType: "xgboost",
		InputFiles: []string{
			"model-settings.json",
			"model.bst",
		},
		ExpectedLinks: []linkExpectation{
			{
				Name: "model.bst",
			},
		},
	},

	// to directory with single .json file with arbitrary name
	{
		ModelID:   "xgboost-single-file-json",
		ModelType: "xgboost",
		InputFiles: []string{
			"arbitrary-name.json",
		},
		ExpectedLinks: []linkExpectation{
			{
				Name:   "arbitrary-name.json",
				Target: "arbitrary-name.json",
			},
		},
	},

	// to directory with single .bst file with arbitrary name
	{
		ModelID:   "xgboost-single-file-bst",
		ModelType: "xgboost",
		InputFiles: []string{
			"arbitrary-name.bst",
		},
		ExpectedLinks: []linkExpectation{
			{
				Name:   "arbitrary-name.bst",
				Target: "arbitrary-name.bst",
			},
		},
	},

	// direct to .json file with arbitrary name
	{
		ModelID:   "xgboost-direct-json",
		ModelType: "xgboost",
		InputFile: "another-arbitrary-name.json",
		ExpectedLinks: []linkExpectation{
			{
				Name:   "another-arbitrary-name.json",
				Target: "another-arbitrary-name.json",
			},
		},
	},

	// direct to file with arbitrary name
	{
		ModelID:   "xgboost-direct-no-extension",
		ModelType: "xgboost",
		InputFile: "yet-another-arbitrary-name",
		ExpectedLinks: []linkExpectation{
			{
				// should default to .json
				Name:   "yet-another-arbitrary-name",
				Target: "yet-another-arbitrary-name",
			},
		},
	},

	// mllib could have multiple directories
	{
		ModelID:   "mllib-standard",
		ModelType: "mllib",
		InputFiles: []string{
			"model-settings.json",
			"data/some-files",
			"metadata/some-files",
		},
		ExpectedLinks: []linkExpectation{
			{
				Name: "data",
			},
			{
				Name: "metadata",
			},
		},
	},
}

func TestRewriteModelDir(t *testing.T) {
	var err error
	// cleanup the entire generatedTestdataDir before running tests
	err = os.RemoveAll(generatedTestdataDir)
	if err != nil {
		t.Fatalf("Could not remove root model dir %s due to error %v", generatedTestdataDir, err)
	}
	for _, tt := range rewriteModelDirTests {
		t.Run(tt.ModelID, func(t *testing.T) {
			var err1 error
			sourceModelIDDir := filepath.Join(generatedTestdataDir, tt.ModelID)
			targetModelIDDir := filepath.Join(generatedMlserverModelsDir, tt.ModelID)
			os.MkdirAll(sourceModelIDDir, 0755)
			// setup the requested files
			if tt.InputFiles != nil {
				for _, f := range tt.InputFiles {
					// a generated config file should have valid JSON content, eg. '{}'
					if f == mlserverRepositoryConfigFilename {
						assertCreateConfigFile(filepath.Join(sourceModelIDDir, f), t)
						continue
					}
					assertCreateEmptyFile(filepath.Join(sourceModelIDDir, f), t)
				}
			} else if tt.InputFile != "" {
				assertCreateEmptyFile(filepath.Join(sourceModelIDDir, tt.InputFile), t)
			} else {
				t.Fatalf("Invalid test case with no input files %v", tt.ModelID)
			}

			// run function under test
			err1 = rewriteModelPath(generatedTestdataDir, tt.ModelID, tt.ModelType, log)
			// assert no error
			if err1 != nil {
				t.Error(err1)
			}

			// assert that the expected links exist and are correct
			generatedFiles, err1 := getFilesInDir(targetModelIDDir)
			if err1 != nil {
				t.Error(err1)
			}
			for _, link := range tt.ExpectedLinks {
				var err2 error
				// assert that the expected file exists
				var expectedFile os.FileInfo = nil
				for _, f := range generatedFiles {
					if f.Name() == link.Name {
						expectedFile = f
					}
				}
				if expectedFile == nil {
					t.Errorf("Expected link [%s] not found in generated files for model %s", link.Name, tt.ModelID)
					continue
				}

				// assert that it is a symlink
				if expectedFile.Mode()&os.ModeSymlink != os.ModeSymlink {
					t.Errorf("Expected [%s] to be a symlink.", expectedFile.Name())
				}

				// assert on the target of the link
				expectedFileFullPath := filepath.Join(generatedMlserverModelsDir, tt.ModelID, expectedFile.Name())
				resolvedPath, err2 := filepath.EvalSymlinks(expectedFileFullPath)
				if err2 != nil {
					t.Errorf("Error resolving symlink [%s]: %w", expectedFileFullPath, err2)
				}
				// the target can be explicit in the expectedLink or is assumed to be
				// the same as the link name
				var expectedTarget string
				if link.Target != "" {
					expectedTarget = link.Target
				} else {
					expectedTarget = link.Name
				}
				expectedTargetFullPath := filepath.Join(sourceModelIDDir, expectedTarget)

				if resolvedPath != expectedTargetFullPath {
					t.Errorf("Expected symlink [%s] to point to [%s] but instead it pointed to [%s]", expectedFileFullPath, expectedTargetFullPath, resolvedPath)
				}

				// assert that the target file exists
				if exists, err2 := fileExists(resolvedPath); !exists {
					if err2 != nil {
						t.Errorf("Expected file %s to exist but got an error checking: %w", resolvedPath, err2)
					} else {
						t.Errorf("Expected file %s to exist but it was not found", resolvedPath)
					}
				}
			}

			assertGeneratedModelDirIsCorrect(sourceModelIDDir, targetModelIDDir, tt.ModelID, t)
		})
	}
	// cleanup the entire generatedTestdataDir after all the tests
	err = os.RemoveAll(generatedTestdataDir)
	if err != nil {
		t.Fatalf("Could not remove root model dir %s due to error %v", generatedTestdataDir, err)
	}
}

func assertCreateEmptyFile(path string, t *testing.T) {
	err := createFile(path, "")
	if err != nil {
		t.Fatal("Unexpected error creating empty file", err)
	}
}

func createFile(path, contents string) error {
	os.MkdirAll(filepath.Dir(path), 0755)
	err := ioutil.WriteFile(path, []byte(contents), 0644)
	return err
}

func assertCreateConfigFile(path string, t *testing.T) {
	err := createFile(path, "{}")
	if err != nil {
		t.Fatal("Unexpected error creating empty file", err)
	}
}

func getFilesInDir(root string) ([]os.FileInfo, error) {
	result := make([]os.FileInfo, 0, 1)
	err := filepath.Walk(root, func(path string, info os.FileInfo, errIn error) error {
		result = append(result, info)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
