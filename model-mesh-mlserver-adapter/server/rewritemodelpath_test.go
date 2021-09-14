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

type linkExpectation struct {
	Name   string
	Target string
}

type rewriteModelDirTestCase struct {
	ModelID        string
	ModelType      string
	InputFiles     []string
	InputConfig    map[string]interface{}
	InputSchema    map[string]interface{}
	ExpectedLinks  []linkExpectation
	ExpectedFiles  []string
	ExpectedConfig map[string]interface{}
	ExpectError    bool
}

func (tt rewriteModelDirTestCase) getSourceDir() string {
	return filepath.Join(generatedTestdataDir, tt.ModelID)
}

func (tt rewriteModelDirTestCase) getTargetDir() string {
	return filepath.Join(generatedMlserverModelsDir, tt.ModelID)
}

func (tt rewriteModelDirTestCase) generateSourceDirectory(t *testing.T) {
	sourceModelIDDir := tt.getSourceDir()
	os.MkdirAll(sourceModelIDDir, 0755)
	// setup the requested files
	for _, f := range tt.InputFiles {
		assertCreateEmptyFile(filepath.Join(sourceModelIDDir, f), t)
	}
	// setup the schema if provided
	if tt.InputSchema != nil {
		tt.writeSchemaFile(t)
	}
	// setup the config if provided
	if tt.InputConfig != nil {
		// assert that "model-settings.json" is included as an input file to avoid
		// confusion with this writing a file that is not part of the model's
		// files
		configInInputFiles := false
		for _, f := range tt.InputFiles {
			if f == "model-settings.json" {
				configInInputFiles = true
			}
		}
		if !configInInputFiles {
			t.Fatalf("Test case %s has InputConfig but model-settings.json is not in InputFiles", tt.ModelID)
		}
		tt.writeConfigFile(t)
	}
}

func (tt rewriteModelDirTestCase) writeConfigFile(t *testing.T) {
	sourceModelIDDir := tt.getSourceDir()
	configFilename := filepath.Join(sourceModelIDDir, "model-settings.json")
	jsonBytes, jerr := json.Marshal(tt.InputConfig)
	if jerr != nil {
		t.Fatal("Error marshalling config JSON", jerr)
	}
	if err := ioutil.WriteFile(configFilename, jsonBytes, 0644); err != nil {
		t.Fatalf("Unable to write config JSON: %v", err)
	}
}

func (tt rewriteModelDirTestCase) writeSchemaFile(t *testing.T) {
	jsonBytes, jerr := json.Marshal(tt.InputSchema)
	if jerr != nil {
		t.Fatal("Error marshalling schema JSON", jerr)
	}

	sourceModelIDDir := tt.getSourceDir()
	schemaFilename := filepath.Join(sourceModelIDDir, "_schema.json")
	if werr := ioutil.WriteFile(schemaFilename, jsonBytes, 0644); werr != nil {
		t.Fatal("Error writing JSON to schema file", werr)
	}
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
		InputConfig: map[string]interface{}{},
		ExpectedLinks: []linkExpectation{
			{
				Name: "model.joblib",
			},
		},
		ExpectedConfig: map[string]interface{}{
			"name": "sklearn-standard-native",
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

	// standard native
	{
		ModelID:   "xgboost-standard-native",
		ModelType: "xgboost",
		InputFiles: []string{
			"model-settings.json",
			"model.bst",
		},
		InputConfig: map[string]interface{}{},
		ExpectedLinks: []linkExpectation{
			{
				Name: "model.bst",
			},
		},
		ExpectedConfig: map[string]interface{}{
			"name": "xgboost-standard-native",
		},
	},

	// standard native with URI
	{
		ModelID:   "xgboost-standard-native",
		ModelType: "xgboost",
		InputFiles: []string{
			"model-settings.json",
			"model.bst",
		},
		InputConfig: map[string]interface{}{
			"name": "some-name",
			"parameters": map[string]interface{}{
				"uri": "./model.bst",
			},
		},
		ExpectedLinks: []linkExpectation{
			{
				Name: "model.bst",
			},
		},
		ExpectedConfig: map[string]interface{}{
			"name": "xgboost-standard-native",
			"parameters": map[string]interface{}{
				"uri": filepath.Join(generatedMlserverModelsDir, "xgboost-standard-native", "model.bst"),
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
		InputFiles: []string{
			"another-arbitrary-name.json",
		},
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
		InputFiles: []string{
			"yet-another-arbitrary-name",
		},
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
		InputConfig: map[string]interface{}{},
		ExpectedLinks: []linkExpectation{
			{
				Name: "data",
			},
			{
				Name: "metadata",
			},
		},
	},

	// schema tests
	{
		ModelID:   "schema-simple",
		ModelType: "xgboost",
		InputFiles: []string{
			"model.json",
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
		ExpectedLinks: []linkExpectation{
			{
				Name:   "model.json",
				Target: "model.json",
			},
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
		ModelID:   "schema-overwrite-outputs",
		ModelType: "xgboost",
		InputFiles: []string{
			"model-settings.json",
			"model.json",
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
		ExpectedLinks: []linkExpectation{
			{
				Name:   "model.json",
				Target: "model.json",
			},
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
			// cleanup the entire generatedTestdataDir before running each test
			err1 = os.RemoveAll(generatedTestdataDir)
			if err1 != nil {
				t.Fatalf("Could not remove root model dir %s due to error %v", generatedTestdataDir, err1)
			}
			tt.generateSourceDirectory(t)

			// run function under test
			err1 = rewriteModelPath(generatedTestdataDir, tt.ModelID, tt.ModelType, log)
			// assert no error
			if err1 != nil {
				t.Error(err1)
			}

			// assert that the expected links exist and are correct
			generatedFiles, err1 := getFilesInDir(tt.getTargetDir())
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
				expectedTargetFullPath := filepath.Join(tt.getSourceDir(), expectedTarget)

				if resolvedPath != expectedTargetFullPath {
					t.Errorf("Expected symlink [%s] to point to [%s] but instead it pointed to [%s]", expectedFileFullPath, expectedTargetFullPath, resolvedPath)
				}

				// assert that the target file exists
				if exists, err2 := util.FileExists(resolvedPath); !exists {
					if err2 != nil {
						t.Errorf("Expected file %s to exist but got an error checking: %w", resolvedPath, err2)
					} else {
						t.Errorf("Expected file %s to exist but it was not found", resolvedPath)
					}
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
			} else {
				// should have `name` field matching the modelID
				if config["name"] != tt.ModelID {
					t.Errorf("Expected `name` parameter to be [%s] but got value of [%s]", tt.ModelID, config["name"])
				}
				// if `uri` parameter exists, it should match the model's full path
				if paramsI, ok := config["parameters"]; ok {
					if params, ok := paramsI.(map[string]interface{}); !ok {
						t.Error("Expected `parameters` to be a map in config")
					} else {
						if uri, ok := params["uri"]; !ok {
							t.Error("Expected `parameters.uri` to exist in config")
						} else {
							if !strings.HasPrefix(uri.(string), tt.getTargetDir()) {
								t.Errorf("Expected `parameters.uri` to be a full path starting with [%s] but got value of [%s]", tt.getTargetDir(), uri)
							}
						}
					}

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
