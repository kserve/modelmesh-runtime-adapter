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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	triton "github.com/kserve/modelmesh-runtime-adapter/internal/proto/triton"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func TestRemoveNameFromModelConfigPbtxt(t *testing.T) {
	pbtxtIn := `platform: "bogus"
name: "REMOVE_ME"
max_batch_size: 1
input [
  {
    name: "INPUT__0"
    data_type: TYPE_UINT8
    dims: [1, 28, 28]
  }
]
output [
  {
    name: "OUTPUT__0"
    data_type: TYPE_FP32
    dims: [10]
  }
]
instance_group [
    {
        count: 1
        kind: KIND_CPU
    }
]
`
	pbtxt := []byte(pbtxtIn)
	var err error
	pbtxt, err = processModelConfig(pbtxt, log, "")

	if err != nil {
		t.Errorf("Expected `name` field to be removed from ModelConfig pbtxt")
	}

	pbtxtOut := string(pbtxt)
	if strings.Contains(pbtxtOut, "REMOVE_ME") {
		t.Errorf("Expected `name` field to be removed from ModelConfig pbtxt")
	}
	// ensure input and output name are not removed
	if !strings.Contains(pbtxtOut, "INPUT__0") {
		t.Errorf("Expected input name to not be removed from ModelConfig pbtxt")
	}
	if !strings.Contains(pbtxtOut, "OUTPUT__0") {
		t.Errorf("Expected output name to not be removed from ModelConfig pbtxt")
	}
}

type rewriteModelPathTestCase struct {
	ModelID            string
	InputModelType     string
	InputFiles         []string
	InputConfig        *triton.ModelConfig
	InputSchema        map[string]interface{}
	ExpectedLinkPath   string
	ExpectedLinkTarget string
	ExpectedFiles      []string
	ExpectedConfig     *triton.ModelConfig
	ExpectError        bool
}

func (tt rewriteModelPathTestCase) getSourceDir() string {
	return filepath.Join(generatedTestdataDir, tt.ModelID)
}

func (tt rewriteModelPathTestCase) getTargetDir() string {
	return filepath.Join(tritonModelsDir, tt.ModelID)
}

func (tt rewriteModelPathTestCase) generateSourceDirectory(t *testing.T) {
	sourceModelIDDir := tt.getSourceDir()
	os.MkdirAll(sourceModelIDDir, 0755)
	// setup the requested files
	for _, f := range tt.InputFiles {
		createEmptyFile(filepath.Join(sourceModelIDDir, f), t)
	}
	// setup the schema if provided
	if tt.InputSchema != nil {
		tt.writeSchemaFile(t)
	}
	// setup the config if provided
	if tt.InputConfig != nil {
		tt.writeConfigFile(t)
	}
}

func (tt rewriteModelPathTestCase) writeConfigFile(t *testing.T) {
	// assert that "config.pbtxt" is included as an input file to avoid
	// confusion with this writing a file that is not part of the model's
	// files
	configInInputFiles := false
	for _, f := range tt.InputFiles {
		if f == "config.pbtxt" {
			configInInputFiles = true
		}
	}
	if !configInInputFiles {
		t.Fatalf("Test case %s has InputConfig but config.pbtxt is not in InputFiles", tt.ModelID)
	}

	sourceModelIDDir := tt.getSourceDir()
	configFilename := filepath.Join(sourceModelIDDir, "config.pbtxt")
	if werr := writeConfigPbtxt(configFilename, tt.InputConfig); werr != nil {
		t.Fatal(werr)
	}
}

func (tt rewriteModelPathTestCase) writeSchemaFile(t *testing.T) {
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

func TestRewriteModelPath(t *testing.T) {
	for _, tt := range rewriteModelPathTests {
		t.Run(tt.ModelID, func(t *testing.T) {
			// cleanup the entire generatedTestdataDir before running each test
			err := os.RemoveAll(generatedTestdataDir)
			if err != nil {
				t.Fatalf("Could not remove root model dir %s due to error %v", generatedTestdataDir, err)
			}
			tt.generateSourceDirectory(t)

			// run function under test
			err = rewriteModelPath(generatedTestdataDir, tt.ModelID, tt.InputModelType, log)

			if tt.ExpectError && err == nil {
				t.Fatal("ExpectError is true, but no error was returned")
			}

			if !tt.ExpectError && err != nil {
				t.Fatal("Did not expect the error", err)
			}

			assertLinkAndPathsExist(t, tt)
			assertConfigFileContents(t, tt)
		})
	}
}

func TestRewriteModelPathMultiple(t *testing.T) {
	// cleanup the entire generated testdata dir now but before each rewriteModelPath only cleanup the source dir so we make sure
	// the rewrite works with existing models in the triton dir
	err := os.RemoveAll(generatedTestdataDir)
	if err != nil {
		t.Fatalf("Could not remove root model dir %s due to error %v", generatedTestdataDir, err)
	}

	// first create all the source files
	for _, tt := range rewriteModelPathTests {
		tt.generateSourceDirectory(t)
	}

	// next run the function under test for all the models
	for _, tt := range rewriteModelPathTests {
		err = rewriteModelPath(generatedTestdataDir, tt.ModelID, tt.InputModelType, log)
		if tt.ExpectError && err == nil {
			t.Fatal("ExpectError is true, but no error was returned")
		}

		if !tt.ExpectError && err != nil {
			t.Fatal("Did not expect the error", err)
		}
	}

	// finally assert all the links and files exist
	for _, tt := range rewriteModelPathTests {
		// t.Logf("Asserting on %s", tt.ModelID) // for debugging
		// skip check if an error was expected
		if tt.ExpectError {
			continue
		}
		assertLinkAndPathsExist(t, tt)
		assertConfigFileContents(t, tt)
	}
}

//
// Helper functions
//

func assertConfigFileContents(t *testing.T, tt rewriteModelPathTestCase) {
	var err error

	// only assert if an expected content is given
	expected := tt.ExpectedConfig
	if expected == nil {
		return
	}

	// read in the generated config file
	targetModelIDDir := filepath.Join(tritonModelsDir, tt.ModelID)
	configFilePath := filepath.Join(targetModelIDDir, "config.pbtxt")
	configFileContents, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		t.Fatalf("Unable to read config file [%s]: %v", configFilePath, err)
	}

	// parse the generated config
	actual := &triton.ModelConfig{}
	err = prototext.Unmarshal(configFileContents, actual)
	if err != nil {
		t.Fatalf("Unable to unmarshal config file [%s]: %v", configFilePath, err)
	}

	if !proto.Equal(expected, actual) {
		t.Fatalf("Expected and actual config file contents do not match.\nExpected: %v\nActual: %v", expected, actual)
	}
}

func assertLinkAndPathsExist(t *testing.T, tt rewriteModelPathTestCase) {
	sourceModelIDDir := tt.getSourceDir()
	targetModelIDDir := tt.getTargetDir()
	symlinks := findSymlinks(targetModelIDDir)
	if len(symlinks) == 0 {
		t.Errorf("Expected symlinks under dir %s but found none", targetModelIDDir)
	}

	// check the links and targets
	// if specified, the expected link must exist and point to the correct target
	expectedLinkPath := tt.ExpectedLinkPath
	expectedLinkTarget := tt.ExpectedLinkTarget

	expectedLinkFound := false
	expectedLinkSpecified := (expectedLinkPath != "")
	expectedLinkFullPath := filepath.Join(targetModelIDDir, expectedLinkPath)
	expectedLinkTargetFullPath := filepath.Join(sourceModelIDDir, expectedLinkTarget)

	for _, linkFullPath := range symlinks {
		resolvedLinkFullPath, err := filepath.EvalSymlinks(linkFullPath)
		if err != nil {
			t.Errorf("Error resolving symlink [%s]: %v", linkFullPath, err)
		}

		// special check if expected link is specified
		if linkFullPath == expectedLinkFullPath && expectedLinkSpecified {
			// verify the symlink points to expected target
			if resolvedLinkFullPath != expectedLinkTargetFullPath {
				t.Errorf("Expected symlink %s to point to %s but instead it pointed to %s", linkFullPath, expectedLinkTargetFullPath, resolvedLinkFullPath)
			}
			expectedLinkFound = true
		}

		// non-sepcial check for other symlinks
		// assert that the target file exists
		if exists, err := fileExists(resolvedLinkFullPath); !exists {
			if err != nil {
				t.Errorf("Expected link target %s to exist but got an error checking: %v", resolvedLinkFullPath, err)
			} else {
				t.Errorf("Expected link target %s to exist but it was not found", resolvedLinkFullPath)
			}
		}
	}

	if expectedLinkSpecified && !expectedLinkFound {
		t.Errorf("Expected to find symlink %s pointing to %s but did not", expectedLinkFullPath, expectedLinkTargetFullPath)
	}

	// assert all the expected files exist
	for _, f := range tt.ExpectedFiles {
		fullExpectedPath := filepath.Join(targetModelIDDir, f)
		if exists, err := fileExists(fullExpectedPath); !exists {
			if err != nil {
				t.Errorf("Expected file %s to exist but got an error checking: %v", fullExpectedPath, err)
			} else {
				t.Errorf("Expected file %s to exist but it was not found", fullExpectedPath)
			}
		}
	}
}

func createEmptyFile(path string, t *testing.T) {
	os.MkdirAll(filepath.Dir(path), 0755)
	emptyFile, err := os.Create(path)
	if err != nil {
		t.Fatal("Unexpected error creating a file", err)
	}
	emptyFile.Close()
}

func findSymlinks(root string) []string {
	result := make([]string, 0, 2)
	err := filepath.Walk(root, func(path string, info os.FileInfo, errIn error) error {
		if info.Mode()&os.ModeSymlink == os.ModeSymlink {
			result = append(result, path)
		}
		return nil
	})
	if err != nil {
		log.Info("Error encountered searching for symlink:", err)
	}
	return result
}

//
// Definition of rewriteModelDir test cases
//

var rewriteModelPathTests = []rewriteModelPathTestCase{
	// Group: tensorflow
	{
		ModelID:        "tensorflowEmpty",
		InputModelType: "tensorflow",
		InputFiles:     []string{
			// no files
		},
		ExpectedLinkPath:   "1/model.savedmodel",
		ExpectedLinkTarget: "",
		ExpectedFiles: []string{
			"1/model.savedmodel",
		},
	},
	{
		ModelID:        "tensorflowSavedModel",
		InputModelType: "tensorflow",
		InputFiles: []string{
			"1/model.savedmodel/saved_model.pb",
		},
		ExpectedLinkPath:   "1/model.savedmodel",
		ExpectedLinkTarget: "1/model.savedmodel",
		ExpectedFiles: []string{
			"1/model.savedmodel/saved_model.pb",
		},
	},
	{
		ModelID:        "tensorflowGraphDef",
		InputModelType: "tensorflow",
		InputFiles: []string{
			"1/model.graphdef",
		},
		ExpectedLinkPath:   "1/model.graphdef",
		ExpectedLinkTarget: "1/model.graphdef",
		ExpectedFiles: []string{
			"1/model.graphdef",
		},
	},
	{
		ModelID:        "tensorflowGraphDefForFile",
		InputModelType: "tensorflow",
		InputFiles: []string{
			"saved_model.pb",
		},
		ExpectedLinkPath:   "1/model.graphdef",
		ExpectedLinkTarget: "saved_model.pb",
		ExpectedFiles: []string{
			"1/model.graphdef",
		},
	},
	{
		ModelID:        "tensorflowSavedModelForDir",
		InputModelType: "tensorflow:1.5",
		InputFiles: []string{
			"saved_model.pb",
			"variables",
		},
		ExpectedLinkPath:   "1/model.savedmodel",
		ExpectedLinkTarget: "",
		ExpectedFiles: []string{
			"1/model.savedmodel/saved_model.pb",
			"1/model.savedmodel/variables",
		},
	},
	{
		ModelID:        "tensorflowAllowSubdir_1",
		InputModelType: "tensorflow",
		InputFiles: []string{
			"saved_model/saved_model.pb",
			"saved_model/variables",
		},
		ExpectedLinkPath:   "1/model.savedmodel",
		ExpectedLinkTarget: "saved_model",
		ExpectedFiles: []string{
			"1/model.savedmodel/saved_model.pb",
			"1/model.savedmodel/variables",
		},
	},
	{
		ModelID:        "tensorflowAllowSubdir_2",
		InputModelType: "tensorflow",
		InputFiles: []string{
			"model.savedmodel/saved_model.pb",
		},
		ExpectedLinkPath:   "1/model.savedmodel",
		ExpectedLinkTarget: "model.savedmodel",
		ExpectedFiles: []string{
			"1/model.savedmodel/saved_model.pb",
		},
	},
	{
		ModelID:        "tensorflowVersionDirWithFile",
		InputModelType: "tensorflow",
		InputFiles: []string{
			"1/saved_model.pb",
		},
		ExpectedLinkPath:   "1/model.graphdef",
		ExpectedLinkTarget: "1/saved_model.pb",
		ExpectedFiles: []string{
			"1/model.graphdef",
		},
	},
	{
		ModelID:        "tensorflowVersionDirWithDir",
		InputModelType: "tensorflow",
		InputFiles: []string{
			"2/saved_model.pb",
			"2/variables",
		},
		ExpectedLinkPath:   "2/model.savedmodel",
		ExpectedLinkTarget: "2",
		ExpectedFiles: []string{
			"2/model.savedmodel/saved_model.pb",
			"2/model.savedmodel/variables",
		},
	},
	{
		ModelID:        "tensorflowSubdirInVersionDir",
		InputModelType: "tensorflow",
		InputFiles: []string{
			"1/saved_model/saved_model.pb",
			"1/saved_model/variables",
		},
		ExpectedLinkPath:   "1/model.savedmodel",
		ExpectedLinkTarget: "1/saved_model",
		ExpectedFiles: []string{
			"1/model.savedmodel/saved_model.pb",
			"1/model.savedmodel/variables",
		},
	},
	{
		ModelID:        "tensorflowAssumeModelDataIfNotAllVersionNumbers",
		InputModelType: "tensorflow",
		// this is a weird case and unlikely to happen, but we assume that this is the model if not all dirs are numbers
		InputFiles: []string{
			"1/model.savedmodel/saved_model.pb",
			"1/model.savedmodel/variables/index",
			"NAN/model.savedmodel/saved_model.pb",
			"NAN/model.savedmodel/variables/index",
		},
		ExpectedLinkPath:   "1/model.savedmodel",
		ExpectedLinkTarget: "",
		ExpectedFiles: []string{
			"1/model.savedmodel/1/model.savedmodel/saved_model.pb",
			"1/model.savedmodel/1/model.savedmodel/variables/index",
			"1/model.savedmodel/NAN/model.savedmodel/saved_model.pb",
			"1/model.savedmodel/NAN/model.savedmodel/variables/index",
		},
	},
	{
		ModelID:        "tensorflowSelectHighVersionSubdir_1",
		InputModelType: "tensorflow",
		InputFiles: []string{
			"1/model.savedmodel/saved_model.pb",
			"1/model.savedmodel/variables/index1",
			"2/model.savedmodel/saved_model.pb",
			"2/model.savedmodel/variables/index2",
			"5/model.savedmodel/saved_model.pb",
			"5/model.savedmodel/variables/index5",
		},
		ExpectedLinkPath:   "5/model.savedmodel",
		ExpectedLinkTarget: "5/model.savedmodel",
		ExpectedFiles: []string{
			"5/model.savedmodel/saved_model.pb",
			"5/model.savedmodel/variables/index5",
		},
	},
	{
		ModelID:        "tensorflowSelectHighVersionSubdir_2",
		InputModelType: "tensorflow",
		InputFiles: []string{
			"2/model.savedmodel/saved_model.pb",
			"2/model.savedmodel/variables/index",
			"14/saved_model_14.pb",
			"14/variables/index_14",
		},
		ExpectedLinkPath:   "14/model.savedmodel",
		ExpectedLinkTarget: "14",
		ExpectedFiles: []string{
			"14/model.savedmodel/saved_model_14.pb",
			"14/model.savedmodel/variables/index_14",
		},
	},
	{
		ModelID:        "tensorflowSelectHighVersionSubdir_3",
		InputModelType: "tensorflow",
		InputFiles: []string{
			"2/model.savedmodel/saved_model.pb",
			"2/model.savedmodel/variables/index",
			"14/saved_model_14.pb",
		},
		ExpectedLinkPath:   "14/model.graphdef",
		ExpectedLinkTarget: "14/saved_model_14.pb",
		ExpectedFiles: []string{
			"14/model.graphdef",
		},
	},
	{
		ModelID:        "tensorflowSavedModelWithConfig",
		InputModelType: "tensorflow",
		InputFiles: []string{
			"1/model.savedmodel/saved_model.pb",
			"config.pbtxt",
		},
		ExpectedLinkPath:   "",
		ExpectedLinkTarget: "",
		ExpectedFiles: []string{
			"1/model.savedmodel/saved_model.pb",
			"config.pbtxt",
		},
	},
	{
		ModelID:        "tensorflowFilesWithConfig",
		InputModelType: "tensorflow",
		InputFiles: []string{
			"1/anything",
			"14/anything/at/all",
			"labels.txt",
			"config.pbtxt",
		},
		ExpectedLinkPath:   "",
		ExpectedLinkTarget: "",
		ExpectedFiles: []string{
			"1/anything",
			"labels.txt",
			"config.pbtxt",
		},
	},

	// Group: tensorrt
	{
		ModelID:        "tensorrtWithVersion",
		InputModelType: "tensorRT",
		InputFiles: []string{
			"1/model.plan",
		},
		ExpectedLinkPath:   "1/model.plan",
		ExpectedLinkTarget: "1/model.plan",
		ExpectedFiles: []string{
			"1/model.plan",
		},
	},
	{
		ModelID:        "tensorrtWithVersionRename",
		InputModelType: "tensorRT:2x",
		InputFiles: []string{
			"2/model_2.plan",
		},
		ExpectedLinkPath:   "2/model.plan",
		ExpectedLinkTarget: "2/model_2.plan",
		ExpectedFiles: []string{
			"2/model.plan",
		},
	},
	{
		ModelID:        "tensorrtSimple",
		InputModelType: "tensorRT",
		InputFiles: []string{
			"model.plan",
		},
		ExpectedLinkPath:   "1/model.plan",
		ExpectedLinkTarget: "model.plan",
		ExpectedFiles: []string{
			"1/model.plan",
		},
	},
	{
		ModelID:        "tensorrtSubdir",
		InputModelType: "tensorRT",
		InputFiles: []string{
			"myModel/model.plan",
		},
		ExpectedLinkPath:   "1",
		ExpectedLinkTarget: "myModel",
		ExpectedFiles: []string{
			"1/model.plan",
		},
	},
	{
		ModelID:        "tensorrtSimpleMultifile",
		InputModelType: "tensorRT",
		InputFiles: []string{
			"model.plan",
			"unexpectedFile",
		},
		ExpectedLinkPath:   "1",
		ExpectedLinkTarget: "",
		ExpectedFiles: []string{
			"1/model.plan",
			"1/unexpectedFile",
		},
	},
	{
		ModelID:        "tensorrtSubdirMultifile",
		InputModelType: "tensorRT",
		InputFiles: []string{
			"myModel/model.plan",
			"myModel/unexpectedFile",
		},
		ExpectedLinkPath:   "1",
		ExpectedLinkTarget: "myModel",
		ExpectedFiles: []string{
			"1/model.plan",
			"1/unexpectedFile",
		},
	},
	{
		ModelID:        "tensorrtUnexpected",
		InputModelType: "tensorRT",
		// This is a case were we don't know what to do, so just add the version, but it will probably fail later on load
		InputFiles: []string{
			"myModel/model.plan",
			"unexpectedDir/unexpectedFile",
		},
		ExpectedLinkPath:   "1",
		ExpectedLinkTarget: "",
		ExpectedFiles: []string{
			"1/myModel/model.plan",
			"1/unexpectedDir/unexpectedFile",
		},
	},

	// Group: pytorch
	{
		ModelID:        "pytorchWithVersion",
		InputModelType: "PyTorch",
		InputFiles: []string{
			"1/model.pt",
		},
		ExpectedLinkPath:   "1/model.pt",
		ExpectedLinkTarget: "1/model.pt",
		ExpectedFiles: []string{
			"1/model.pt",
		},
	},
	{
		ModelID:        "pytorchSimpleRename",
		InputModelType: "PyTorch",
		InputFiles: []string{
			"model.plan",
		},
		ExpectedLinkPath:   "1/model.pt",
		ExpectedLinkTarget: "model.plan",
		ExpectedFiles: []string{
			"1/model.pt",
		},
	},

	// Group: onnx
	{
		ModelID:        "onnxWithVersionFile",
		InputModelType: "onnx",
		InputFiles: []string{
			"1/model.onnx",
		},
		ExpectedLinkPath:   "1/model.onnx",
		ExpectedLinkTarget: "1/model.onnx",
		ExpectedFiles: []string{
			"1/model.onnx",
		},
	},
	{
		ModelID:        "onnxWithVersionDir",
		InputModelType: "onnx",
		InputFiles: []string{
			"1/model.onnx/model.onnx",
		},
		ExpectedLinkPath:   "1/model.onnx",
		ExpectedLinkTarget: "1/model.onnx",
		ExpectedFiles: []string{
			"1/model.onnx/model.onnx",
		},
	},
	{
		ModelID:        "onnxWithVersionDirMultifile",
		InputModelType: "onnx",
		InputFiles: []string{
			"1/model.onnx/model.onnx",
			"1/model.onnx/modelfile.txt",
		},
		ExpectedLinkPath:   "1/model.onnx",
		ExpectedLinkTarget: "1/model.onnx",
		ExpectedFiles: []string{
			"1/model.onnx/model.onnx",
			"1/model.onnx/modelfile.txt",
		},
	},
	{
		ModelID:        "onnxSimpleRename",
		InputModelType: "onnx",
		InputFiles: []string{
			"model.x",
		},
		ExpectedLinkPath:   "1/model.onnx",
		ExpectedLinkTarget: "model.x",
		ExpectedFiles: []string{
			"1/model.onnx",
		},
	},
	{
		ModelID:        "onnxMultifile",
		InputModelType: "onnx",
		InputFiles: []string{
			"model.x",
			"variables",
		},
		ExpectedLinkPath:   "1/model.onnx",
		ExpectedLinkTarget: "",
		ExpectedFiles: []string{
			"1/model.onnx/model.x",
			"1/model.onnx/variables",
		},
	},
	{
		ModelID:        "onnxSimpleSubdir",
		InputModelType: "onnx",
		InputFiles: []string{
			"myModel/model.onnx",
		},
		ExpectedLinkPath:   "1/model.onnx",
		ExpectedLinkTarget: "myModel",
		ExpectedFiles: []string{
			"1/model.onnx/model.onnx",
		},
	},
	{
		ModelID:        "onnxSimpleSubdirMultifile",
		InputModelType: "onnx",
		InputFiles: []string{
			"myModel/model.onnx",
			"myModel/variables",
		},
		ExpectedLinkPath:   "1/model.onnx",
		ExpectedLinkTarget: "myModel",
		ExpectedFiles: []string{
			"1/model.onnx/model.onnx",
			"1/model.onnx/variables",
		},
	},

	// Group: unknown model type
	{
		ModelID:        "unknownSubdir",
		InputModelType: "unknown",
		InputFiles: []string{
			"myModel/model.onnx",
			"myModel/variables",
		},
		ExpectedLinkPath:   "1/myModel",
		ExpectedLinkTarget: "myModel",
		ExpectedFiles: []string{
			"1/myModel/model.onnx",
			"1/myModel/variables",
		},
	},
	{
		ModelID:        "unknownSimple",
		InputModelType: "unknown",
		InputFiles: []string{
			"model.onnx",
		},
		ExpectedLinkPath:   "1/model.onnx",
		ExpectedLinkTarget: "model.onnx",
		ExpectedFiles: []string{
			"1/model.onnx",
		},
	},
	{
		ModelID:        "unknownWithVersion",
		InputModelType: "unknown",
		InputFiles: []string{
			"2/model.onnx",
			"2/variables",
		},
		ExpectedLinkPath:   "2",
		ExpectedLinkTarget: "2",
		ExpectedFiles: []string{
			"2/model.onnx",
			"2/variables",
		},
	},
	{
		ModelID:        "unknownMultifile",
		InputModelType: "unknown",
		InputFiles: []string{
			"model.onnx",
			"variables",
		},
		ExpectedLinkPath:   "1",
		ExpectedLinkTarget: "",
		ExpectedFiles: []string{
			"1/model.onnx",
			"1/variables",
		},
	},

	// Group: schema
	{
		ModelID:        "schemaOnnxSimpleRename",
		InputModelType: "onnx",
		InputSchema:    map[string]interface{}{},
		InputFiles: []string{
			"my-model.onnx",
		},
		ExpectedLinkPath:   "1/model.onnx",
		ExpectedLinkTarget: "my-model.onnx",
		ExpectedFiles: []string{
			"1/model.onnx",
			"config.pbtxt",
		},
		ExpectedConfig: &triton.ModelConfig{
			Backend: "onnxruntime",
		},
	},
	{
		ModelID:        "schemaPytorchSimple",
		InputModelType: "pytorch",
		InputSchema: map[string]interface{}{
			"inputs": []map[string]interface{}{
				{
					"name":     "INPUT",
					"datatype": "FP32",
					"shape":    []int{3, 32, 32},
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
		InputFiles: []string{
			"my-model.pt",
		},
		ExpectedLinkPath:   "1/model.pt",
		ExpectedLinkTarget: "my-model.pt",
		ExpectedFiles: []string{
			"1/model.pt",
			"config.pbtxt",
		},
		ExpectedConfig: &triton.ModelConfig{
			Backend: "pytorch",
			Input: []*triton.ModelInput{
				{
					Name:     "INPUT",
					DataType: triton.DataType_TYPE_FP32,
					Dims:     []int64{3, 32, 32},
				},
			},
			Output: []*triton.ModelOutput{
				{
					Name:     "OUTPUT",
					DataType: triton.DataType_TYPE_INT64,
					Dims:     []int64{1},
				},
			},
		},
	},
	{
		ModelID:        "schemaTensorflowSavedModelSimple",
		InputModelType: "tensorflow",
		InputSchema: map[string]interface{}{
			"inputs": []map[string]interface{}{
				{
					"name":     "INPUT",
					"datatype": "FP32",
					"shape":    []int{3, 32, 32},
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
		InputFiles: []string{
			"saved_model.pb",
			"variables",
		},
		ExpectedLinkPath:   "1/model.savedmodel",
		ExpectedLinkTarget: "",
		ExpectedFiles: []string{
			"1/model.savedmodel/saved_model.pb",
			"1/model.savedmodel/variables",
			"config.pbtxt",
		},
		ExpectedConfig: &triton.ModelConfig{
			Backend: "tensorflow",
			Input: []*triton.ModelInput{
				{
					Name:     "INPUT",
					DataType: triton.DataType_TYPE_FP32,
					Dims:     []int64{3, 32, 32},
				},
			},
			Output: []*triton.ModelOutput{
				{
					Name:     "OUTPUT",
					DataType: triton.DataType_TYPE_INT64,
					Dims:     []int64{1},
				},
			},
		},
	},
	{
		ModelID:        "schemaOverwritesConfig",
		InputModelType: "tensorflow",
		InputSchema: map[string]interface{}{
			"inputs": []map[string]interface{}{
				{
					"name":     "NEW_INPUT",
					"datatype": "FP32",
					"shape":    []int{1, 64, 64},
				},
			},
		},
		InputConfig: &triton.ModelConfig{
			Name:    "this-should-be-removed",
			Backend: "tensorflow",
			Input: []*triton.ModelInput{
				{
					Name:     "SOME_INPUT",
					DataType: triton.DataType_TYPE_FP32,
					Dims:     []int64{3, 32, 32},
				},
			},
			Output: []*triton.ModelOutput{
				{
					Name:     "OUTPUT",
					DataType: triton.DataType_TYPE_INT64,
					Dims:     []int64{1},
				},
			},
		},
		InputFiles: []string{
			"1/model.graphdef",
			"some-other-file",
			"config.pbtxt",
		},
		ExpectedLinkPath:   "",
		ExpectedLinkTarget: "",
		ExpectedFiles: []string{
			"1/model.graphdef",
			"some-other-file",
			"config.pbtxt",
		},
		ExpectedConfig: &triton.ModelConfig{
			Backend: "tensorflow",
			Input: []*triton.ModelInput{
				{
					Name:     "NEW_INPUT",
					DataType: triton.DataType_TYPE_FP32,
					Dims:     []int64{1, 64, 64},
				},
			},
			Output: []*triton.ModelOutput{
				{
					Name:     "OUTPUT",
					DataType: triton.DataType_TYPE_INT64,
					Dims:     []int64{1},
				},
			},
		},
	},

	// Group: schema edge-cases
	{
		ModelID:        "schemaEdgeMaxBatchSize",
		InputModelType: "pytorch",
		// schema for model that supports batching
		InputSchema: map[string]interface{}{
			"inputs": []map[string]interface{}{
				{
					"name":     "INPUT",
					"datatype": "FP32",
					"shape":    []int{-1, 64, 64},
				},
				{
					"name":     "TYPE",
					"datatype": "INT8",
					"shape":    []int{-1, 1},
				},
			},
			"outputs": []map[string]interface{}{
				{
					"name":     "OUTPUT",
					"datatype": "INT64",
					"shape":    []int{-1, 10},
				},
			},
		},
		InputConfig: &triton.ModelConfig{
			Backend:      "pytorch",
			MaxBatchSize: 100,
		},
		InputFiles: []string{
			"1/model.pt",
			"config.pbtxt",
		},
		ExpectedLinkPath:   "",
		ExpectedLinkTarget: "",
		ExpectedFiles: []string{
			"1/model.pt",
			"config.pbtxt",
		},
		ExpectedConfig: &triton.ModelConfig{
			Backend:      "pytorch",
			MaxBatchSize: 100,
			Input: []*triton.ModelInput{
				{
					Name:     "INPUT",
					DataType: triton.DataType_TYPE_FP32,
					Dims:     []int64{64, 64},
				},
				{
					Name:     "TYPE",
					DataType: triton.DataType_TYPE_INT8,
					Dims:     []int64{1},
				},
			},
			Output: []*triton.ModelOutput{
				{
					Name:     "OUTPUT",
					DataType: triton.DataType_TYPE_INT64,
					Dims:     []int64{10},
				},
			},
		},
	},
	{
		ModelID:        "schemaEdgeMaxBatchSizeError",
		InputModelType: "pytorch",
		// schema for model that supports batching, but output is missing batch dim
		InputSchema: map[string]interface{}{
			"inputs": []map[string]interface{}{
				{
					"name":     "INPUT",
					"datatype": "FP32",
					"shape":    []int{-1, 64, 64},
				},
			},
			"outputs": []map[string]interface{}{
				{
					"name":     "OUTPUT",
					"datatype": "INT64",
					"shape":    []int{10},
				},
			},
		},
		InputConfig: &triton.ModelConfig{
			MaxBatchSize: 100,
		},
		InputFiles: []string{
			"1/model.pt",
			"config.pbtxt",
		},
		ExpectError: true,
	},
}
