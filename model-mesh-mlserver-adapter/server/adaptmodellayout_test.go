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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
)

var generatedTestdataDir = filepath.Join(testdataDir, "generated")
var generatedMlserverModelsDir string = filepath.Join(generatedTestdataDir, mlserverModelSubdir)

type adaptModelLayoutTestCase struct {
	ModelID        string
	ModelType      string
	ModelPath      string
	SchemaPath     string
	InputFiles     []string
	InputConfig    map[string]interface{}
	InputSchema    map[string]interface{}
	ExpectedFiles  []string
	ExpectedConfig map[string]interface{}
	ExpectError    bool
}

func (tt adaptModelLayoutTestCase) getSourceDir() string {
	return filepath.Join(generatedTestdataDir, tt.ModelID)
}

func (tt adaptModelLayoutTestCase) getTargetDir() string {
	return filepath.Join(generatedMlserverModelsDir, tt.ModelID)
}

func (tt adaptModelLayoutTestCase) generateSourceDirectory(t *testing.T) {
	sourceModelIDDir := tt.getSourceDir()
	os.MkdirAll(sourceModelIDDir, 0755)
	// setup the requested files
	for _, f := range tt.InputFiles {
		assertCreateEmptyFile(filepath.Join(sourceModelIDDir, f), t)
	}
	// setup the schema if provided
	if tt.InputSchema != nil {
		// assert that SchemaPath is set
		if tt.SchemaPath == "" {
			t.Fatalf("Test case %s has InputSchema but SchemaPath is unset", tt.ModelID)
		}
		// assert that the file pointed at by schema path is included as
		// an input file to avoid confusion
		schemaInInputFiles := false
		for _, f := range tt.InputFiles {
			if f == tt.SchemaPath {
				schemaInInputFiles = true
			}
		}
		if !schemaInInputFiles {
			t.Fatalf("Test case %s has file pointed at by SchemaPath that is not in InputFiles", tt.ModelID)
		}
		tt.writeSchemaFile(t)
	}
	// setup the config if provided
	if tt.InputConfig != nil {
		// assert that "model-settings.json" is included as an input
		// file to avoid confusion
		configInInputFiles := false
		for _, f := range tt.InputFiles {
			if f == filepath.Join(tt.ModelPath, "model-settings.json") {
				configInInputFiles = true
			}
		}
		if !configInInputFiles {
			t.Fatalf("Test case %s has InputConfig but model-settings.json is not in InputFiles", tt.ModelID)
		}
		tt.writeConfigFile(t)
	}
}

func (tt adaptModelLayoutTestCase) writeConfigFile(t *testing.T) {
	configFullPath := filepath.Join(tt.getSourceDir(), tt.ModelPath, "model-settings.json")
	jsonBytes, jerr := json.Marshal(tt.InputConfig)
	if jerr != nil {
		t.Fatal("Error marshalling config JSON", jerr)
	}
	if err := ioutil.WriteFile(configFullPath, jsonBytes, 0644); err != nil {
		t.Fatalf("Unable to write config JSON: %v", err)
	}
}

func (tt adaptModelLayoutTestCase) writeSchemaFile(t *testing.T) {
	jsonBytes, jerr := json.Marshal(tt.InputSchema)
	if jerr != nil {
		t.Fatal("Error marshalling schema JSON", jerr)
	}

	schemaFullPath := filepath.Join(tt.getSourceDir(), tt.SchemaPath)
	if werr := ioutil.WriteFile(schemaFullPath, jsonBytes, 0644); werr != nil {
		t.Fatal("Error writing JSON to schema file", werr)
	}
}

var adaptModelLayoutTests = []adaptModelLayoutTestCase{
	// Group: file layout / model path support

	// path to single file
	{
		ModelID:   "single-file",
		ModelType: "custom",
		ModelPath: "some-file",
		InputFiles: []string{
			"some-file",
		},
		ExpectedFiles: []string{
			"model-settings.json",
			"some-file",
		},
	},

	// path to a directory with a single file
	{
		ModelID:   "dir-with-single-file",
		ModelType: "custom",
		ModelPath: "somedir",
		InputFiles: []string{
			"somedir/somefile",
		},
		ExpectedFiles: []string{
			"model-settings.json",
			"somedir",
		},
	},

	// path to a directory with a mix of files and dirs
	{
		ModelID:   "dir-with-complex-structure",
		ModelType: "custom",
		ModelPath: "modelname",
		InputFiles: []string{
			"modelname/data",
			"modelname/metadata",
			"modelname/somedir/somefile1",
			"modelname/somedir/somefile2",
			"modelname/anotherdir/morefiles",
		},
		ExpectedFiles: []string{
			"model-settings.json",
			"modelname",
		},
	},

	// path to a standard MLServer directory
	{
		ModelID:   "dir-mlserver-layout",
		ModelType: "custom",
		ModelPath: "",
		InputFiles: []string{
			"model-settings.json",
			"somefile",
			"somedir/data",
		},
		InputConfig: map[string]interface{}{},
		ExpectedFiles: []string{
			"model-settings.json",
			"somefile",
			"somedir",
		},
	},

	// backwards compatibility with path to model id directory
	{
		ModelID:   "path-to-modelid-dir",
		ModelType: "custom",
		ModelPath: "",
		InputFiles: []string{
			"somefile",
		},
		ExpectedFiles: []string{
			"model-settings.json",
			"path-to-modelid-dir",
		},
	},

	// Group: native model types and layouts

	{
		ModelID:   "sklearn-standard-native",
		ModelType: "sklearn",
		ModelPath: "model",
		InputFiles: []string{
			"model/model-settings.json",
			"model/model.joblib",
		},
		InputConfig: map[string]interface{}{
			"name":           "my sklearn model",
			"implementation": "mlserver_sklearn.SKLearnModel",
		},
		ExpectedFiles: []string{
			"model-settings.json",
			"model.joblib",
		},
		ExpectedConfig: map[string]interface{}{
			"name":           "sklearn-standard-native",
			"implementation": "mlserver_sklearn.SKLearnModel",
		},
	},

	// xgboost standard native
	{
		ModelID:   "xgboost-standard-native",
		ModelType: "xgboost",
		ModelPath: "data",
		InputFiles: []string{
			"data/model-settings.json",
			"data/model.bst",
		},
		InputConfig: map[string]interface{}{
			"implementation": "mlserver_xgboost.XGBoostModel",
		},
		ExpectedFiles: []string{
			"model-settings.json",
			"model.bst",
		},
		ExpectedConfig: map[string]interface{}{
			"name":           "xgboost-standard-native",
			"implementation": "mlserver_xgboost.XGBoostModel",
		},
	},

	// xgboost example model
	{
		ModelID:   "xgboost-example",
		ModelType: "xgboost",
		ModelPath: "dir",
		InputFiles: []string{
			"dir/model-settings.json",
			"dir/mushroom-xgboost.json",
		},
		InputConfig: map[string]interface{}{
			"name":           "mushroom-xgboost",
			"implementation": "mlserver_xgboost.XGBoostModel",
			"parameters": map[string]interface{}{
				"uri":     "./mushroom-xgboost.json",
				"version": "v0.1.0",
			},
		},
		ExpectedFiles: []string{
			"model-settings.json",
			"mushroom-xgboost.json",
		},
		ExpectedConfig: map[string]interface{}{
			"name":           "xgboost-example",
			"implementation": "mlserver_xgboost.XGBoostModel",
			"parameters": map[string]interface{}{
				"uri":     filepath.Join(generatedMlserverModelsDir, "xgboost-example", "mushroom-xgboost.json"),
				"version": "v0.1.0",
			},
		},
	},

	// mllib could have multiple directories
	{
		ModelID:   "mllib-standard",
		ModelType: "mllib",
		ModelPath: "mllib",
		InputFiles: []string{
			"mllib/model-settings.json",
			"mllib/data/some-files",
			"mllib/metadata/some-files",
		},
		InputConfig: map[string]interface{}{},
		ExpectedFiles: []string{
			"model-settings.json",
			"data",
			"metadata",
		},
		ExpectedConfig: map[string]interface{}{
			"name": "mllib-standard",
		},
	},

	// Group: schema tests

	{
		ModelID:    "schema-simple",
		ModelType:  "xgboost",
		ModelPath:  "model.json",
		SchemaPath: "_schema.json",
		InputFiles: []string{
			"model.json",
			"_schema.json",
		},
		InputSchema: map[string]interface{}{
			"inputs": []map[string]interface{}{
				{
					"name":     "INPUT",
					"datatype": "FP32",
					"shape":    []int{1, 784},
				},
			},
			"outputs": []map[string]interface{}{
				{
					"name":     "OUTPUT",
					"datatype": "INT64",
					"shape":    []int{1},
				},
			},
		},
		ExpectedFiles: []string{
			"model-settings.json",
			"model.json",
		},
		ExpectedConfig: map[string]interface{}{
			"name":           "schema-simple",
			"implementation": "mlserver_xgboost.XGBoostModel",
			"parameters": map[string]interface{}{
				"uri": filepath.Join(generatedMlserverModelsDir, "schema-simple", "model.json"),
			},
			"inputs": []map[string]interface{}{
				{
					"name":     "INPUT",
					"datatype": "FP32",
					"shape":    []int{1, 784},
				},
			},
			"outputs": []map[string]interface{}{
				{
					"name":     "OUTPUT",
					"datatype": "INT64",
					"shape":    []int{1},
				},
			},
		},
	},
	{
		ModelID:    "schema-overwrite-outputs",
		ModelType:  "xgboost",
		ModelPath:  "my-model",
		SchemaPath: "_schema.json",
		InputFiles: []string{
			"my-model/model-settings.json",
			"my-model/model.json",
			"_schema.json",
		},
		InputConfig: map[string]interface{}{
			"name": "some-name",
			"inputs": []map[string]interface{}{
				{
					"name":     "INPUT",
					"datatype": "FP32",
					"shape":    []int{1, 784},
				},
			},
			"outputs": []map[string]interface{}{
				{
					"name":     "OUTPUT",
					"datatype": "INT64",
					"shape":    []int{1},
				},
			},
		},
		InputSchema: map[string]interface{}{
			"outputs": []map[string]interface{}{
				{
					"name":     "NEW_OUTPUT",
					"datatype": "FP32",
					"shape":    []int{1, 1},
				},
			},
		},
		ExpectedFiles: []string{
			"model-settings.json",
			"model.json",
		},
		ExpectedConfig: map[string]interface{}{
			"name": "schema-overwrite-outputs",
			"inputs": []map[string]interface{}{
				{
					"name":     "INPUT",
					"datatype": "FP32",
					"shape":    []int{1, 784},
				},
			},
			"outputs": []map[string]interface{}{
				{
					"name":     "NEW_OUTPUT",
					"datatype": "FP32",
					"shape":    []int{1, 1},
				},
			},
		},
	},
}

func TestAdaptModelLayoutForRuntime(t *testing.T) {
	var err error
	// cleanup the entire generatedTestdataDir before running tests
	err = os.RemoveAll(generatedTestdataDir)
	if err != nil {
		t.Fatalf("Could not remove root model dir %s due to error %v", generatedTestdataDir, err)
	}
	mlServerRootModelDir := filepath.Join(generatedTestdataDir, mlserverModelSubdir)
	for _, tt := range adaptModelLayoutTests {
		t.Run(tt.ModelID, func(t *testing.T) {
			var err1 error
			// cleanup the entire generatedTestdataDir before running each test
			err1 = os.RemoveAll(generatedTestdataDir)
			if err1 != nil {
				t.Fatalf("Could not remove root model dir %s due to error %v", generatedTestdataDir, err1)
			}
			tt.generateSourceDirectory(t)

			// run function under test
			modelFullPath := filepath.Join(tt.getSourceDir(), tt.ModelPath)
			schemaFullPath := ""
			if tt.SchemaPath != "" {
				schemaFullPath = filepath.Join(tt.getSourceDir(), tt.SchemaPath)
			}
			err1 = adaptModelLayoutForRuntime(mlServerRootModelDir, tt.ModelID, tt.ModelType, modelFullPath, schemaFullPath, log)

			// assert no error
			if err1 != nil {
				t.Fatal("adaptModelLayoutForRuntime failed with error:", err1)
			}

			// assert that the expected links exist and are correct
			generatedFiles, err1 := getFilesInDir(tt.getTargetDir())
			if err1 != nil {
				t.Fatal(err1)
			}
			for _, file := range tt.ExpectedFiles {
				// assert that the expected file exists
				var expectedFile os.FileInfo = nil
				for _, f := range generatedFiles {
					if f.Name() == file {
						expectedFile = f
					}
				}
				if expectedFile == nil {
					t.Errorf("Expected file [%s] not found in generated files for model %s", file, tt.ModelID)
					continue
				}

				// models settings file is checked below, so skip it here
				if expectedFile.Name() == "model-settings.json" {
					continue
				}

				// assert that it is a symlink,
				if expectedFile.Mode()&os.ModeSymlink != os.ModeSymlink {
					t.Errorf("Expected [%s] to be a symlink.", expectedFile.Name())
				}

				// assert on the target of the link
				linkFullPath := filepath.Join(generatedMlserverModelsDir, tt.ModelID, expectedFile.Name())
				resolvedPath, err2 := filepath.EvalSymlinks(linkFullPath)
				if err2 != nil {
					t.Errorf("Error resolving symlink [%s]: %v", linkFullPath, err2)
				}
				// assert that the target file exists
				if exists, err2 := util.FileExists(resolvedPath); !exists {
					if err2 != nil {
						t.Errorf("Expected file %s to exist but got an error checking: %v", resolvedPath, err2)
					} else {
						t.Errorf("Expected file %s to exist but it was not found", resolvedPath)
					}
				}
				// and points to a file with the same name
				if filepath.Base(resolvedPath) != filepath.Base(linkFullPath) {
					t.Errorf("Expected symlink [%s] to point to [%s] but instead it pointed to [%s]", linkFullPath, filepath.Base(resolvedPath), filepath.Base(linkFullPath))
				}

			}

			// assert on the config file
			// config file must exist in the generated dir, so we track if we found it
			configFilePath := ""
			for _, f := range generatedFiles {
				filePath := filepath.Join(tt.getTargetDir(), f.Name())
				if f.Name() == mlserverRepositoryConfigFilename {
					configFilePath = filePath
					break
				}
			}

			if configFilePath == "" {
				t.Fatalf("Did not find config file [%s] in [%s]", mlserverRepositoryConfigFilename, tt.getTargetDir())
			}

			// read in the config to assert on it
			configJSON, err2 := ioutil.ReadFile(configFilePath)
			if err2 != nil {
				t.Fatalf("Unable to read config file %s: %v", configFilePath, err2)
			}

			var config map[string]interface{}
			if err2 = json.Unmarshal(configJSON, &config); err2 != nil {
				t.Errorf("Unable to unmarshal config file: %v", err2)
			}

			// should have `name` field matching the modelID
			if config["name"] != tt.ModelID {
				t.Errorf("Expected `name` parameter to be [%s] but got value of [%s]", tt.ModelID, config["name"])
			}

			if tt.ExpectedConfig != nil {
				// marshal to JSON strings and compare
				expected, err2 := json.Marshal(tt.ExpectedConfig)
				if err2 != nil {
					t.Fatalf("Unable to marshal expected config: %v", err2)
				}

				actual, err2 := json.Marshal(config)
				if err2 != nil {
					t.Fatalf("Unable to re-marshal config: %v", err2)
				}

				if len(expected) != len(actual) {
					t.Errorf("Expected and actual configs do not have the same serialized length. Expected [%d] but got [%d].", len(expected), len(actual))
					fmt.Printf("Expected: %s\n", string(expected))
					fmt.Printf("Actual  : %s\n", string(actual))
				}
				for i, v := range expected {
					if v != actual[i] {
						t.Errorf("Expected and actual configs do not match at byte %d. Expected [%c] but got [%c].", i, v, actual[i])
						// break at first mismatch
						break
					}
				}
			} else if tt.InputConfig == nil {
				// for a generated config `parameters.uri` matches the model's full path
				uri, ok := extractURI(config)
				if !ok {
					t.Errorf("Expected `parameters.uri` to exist in the config")
				}

				// if tt.ModelPath is empty, we expect the URI to point to the model id dir
				var uriTarget string
				if tt.ModelPath == "" {
					uriTarget = tt.ModelID
				} else {
					uriTarget = filepath.Base(tt.ModelPath)
				}
				expectedURI := filepath.Join(tt.getTargetDir(), uriTarget)
				if uri != expectedURI {
					t.Errorf("Expected `parameters.uri` to be [%s] but got [%s]", expectedURI, uri)
				}
			} else if uri, ok := extractURI(config); ok {
				// for an adapted config that had a URI, check that the uri is a full path to the target dir
				if _, ok1 := extractURI(tt.InputConfig); ok1 {
					if !strings.HasPrefix(uri, tt.getTargetDir()) {
						t.Errorf("Expected `parameters.uri` to be a full path starting with [%s] but got [%s]", tt.getTargetDir(), uri)
					}
				} else {
					// if the config had no value for uri, then the adapated config should not have it
					t.Errorf("Expected `parameters.uri` to not exist in adapted config but got [%s]", uri)
				}
			}
		})
	}
	// cleanup the entire generatedTestdataDir after all the tests
	err = os.RemoveAll(generatedTestdataDir)
	if err != nil {
		t.Fatalf("Could not remove root model dir %s due to error %v", generatedTestdataDir, err)
	}
}

func extractURI(config map[string]interface{}) (string, bool) {
	var uri string
	if paramsI, ok := config["parameters"]; !ok {
		return "", false
	} else if params, ok := paramsI.(map[string]interface{}); !ok {
		return "", false
	} else {
		if pURI, ok := params["uri"]; !ok {
			return "", false
		} else {
			uri, ok = pURI.(string)
			return uri, ok
		}
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
