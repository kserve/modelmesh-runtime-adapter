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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	triton "github.com/kserve/modelmesh-runtime-adapter/internal/proto/triton"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
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
	pbtxt, err = processModelConfig(pbtxt, "", log)

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

type adaptModelLayoutTestCase struct {
	ModelID            string
	ModelType          string
	ModelPath          string
	SchemaPath         string
	InputFiles         []string
	InputConfig        *triton.ModelConfig
	InputSchema        map[string]interface{}
	ExpectedLinkPath   string
	ExpectedLinkTarget string
	ExpectedFiles      []string
	ExpectedConfig     *triton.ModelConfig
	ExpectError        bool
}

func (tt adaptModelLayoutTestCase) getSourceDir() string {
	return filepath.Join(generatedTestdataDir, tt.ModelID)
}

func (tt adaptModelLayoutTestCase) getTargetDir() string {
	return filepath.Join(tritonModelsDir, tt.ModelID)
}

func (tt adaptModelLayoutTestCase) generateSourceDirectory(t *testing.T) {
	sourceModelIDDir := tt.getSourceDir()
	os.MkdirAll(sourceModelIDDir, 0755)

	// setup the requested files
	for _, f := range tt.InputFiles {
		createEmptyFile(filepath.Join(sourceModelIDDir, f), t)
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
		// assert that "config.pbtxt" is included as an input file to
		// avoid confusion
		configInInputFiles := false
		for _, f := range tt.InputFiles {
			if f == filepath.Join(tt.ModelPath, "config.pbtxt") {
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

func (tt adaptModelLayoutTestCase) writeSchemaFile(t *testing.T) {
	jsonBytes, jerr := json.Marshal(tt.InputSchema)
	if jerr != nil {
		t.Fatal("Error marshalling schema JSON", jerr)
	}

	schemaFullpath := filepath.Join(tt.getSourceDir(), tt.SchemaPath)
	if werr := ioutil.WriteFile(schemaFullpath, jsonBytes, 0644); werr != nil {
		t.Fatal("Error writing JSON to schema file", werr)
	}
}

func TestAdaptModelLayoutForRuntime(t *testing.T) {
	tritonRootModelDir := filepath.Join(generatedTestdataDir, tritonModelSubdir)
	for _, tt := range adaptModelLayoutTests {
		t.Run(tt.ModelID, func(t *testing.T) {
			// cleanup the source directory before running each test
			err := os.RemoveAll(tt.getSourceDir())
			if err != nil {
				t.Fatalf("Could not remove root model dir %s due to error %v", generatedTestdataDir, err)
			}
			tt.generateSourceDirectory(t)

			// run function under test
			modelFullPath := filepath.Join(tt.getSourceDir(), tt.ModelPath)
			schemaFullPath := ""
			if tt.SchemaPath != "" {
				schemaFullPath = filepath.Join(tt.getSourceDir(), tt.SchemaPath)
			}
			err = adaptModelLayoutForRuntime(context.Background(), tritonRootModelDir, tt.ModelID, tt.ModelType, modelFullPath, schemaFullPath, log)

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

func TestAdaptModelLayoutForRuntime_Multiple(t *testing.T) {
	// cleanup the entire generated testdata dir now instead of before each
	// test case so we make sure the function works with existing models in
	// the triton dir
	err := os.RemoveAll(generatedTestdataDir)
	if err != nil {
		t.Fatalf("Could not remove root model dir %s due to error %v", generatedTestdataDir, err)
	}
	tritonRootModelDir := filepath.Join(generatedTestdataDir, tritonModelSubdir)

	// first create all the source files
	for _, tt := range adaptModelLayoutTests {
		tt.generateSourceDirectory(t)
	}

	// next run the function under test for all the models
	ctx := context.Background()
	for _, tt := range adaptModelLayoutTests {
		modelFullPath := filepath.Join(tt.getSourceDir(), tt.ModelPath)
		schemaFullPath := ""
		if tt.SchemaPath != "" {
			schemaFullPath = filepath.Join(tt.getSourceDir(), tt.SchemaPath)
		}
		err = adaptModelLayoutForRuntime(ctx, tritonRootModelDir, tt.ModelID, tt.ModelType, modelFullPath, schemaFullPath, log)
		if tt.ExpectError && err == nil {
			t.Fatal("ExpectError is true, but no error was returned")
		}

		if !tt.ExpectError && err != nil {
			t.Fatal("Did not expect the error", err)
		}
	}

	// finally assert all the links and files exist
	for _, tt := range adaptModelLayoutTests {
		// t.Logf("Asserting on %s", tt.ModelID) // for debugging
		// skip check if an error was expected
		if tt.ExpectError {
			continue
		}
		assertLinkAndPathsExist(t, tt)
		assertConfigFileContents(t, tt)
	}
}

// If running as a suite, remove the generated files
func TestCleanupGeneratedDir(t *testing.T) {
	err := os.RemoveAll(generatedTestdataDir)
	if err != nil {
		fmt.Printf("Could not remove generated model dir %s due to error %v\n", generatedTestdataDir, err)
	}
}

//
// Helper functions
//

func assertConfigFileContents(t *testing.T, tt adaptModelLayoutTestCase) {
	var err error

	// only assert if an expected content is given
	expected := tt.ExpectedConfig
	if expected == nil {
		return
	}

	// read in the generated config file
	configFilePath := filepath.Join(tt.getTargetDir(), "config.pbtxt")
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

func assertLinkAndPathsExist(t *testing.T, tt adaptModelLayoutTestCase) {
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
		if exists, err := util.FileExists(resolvedLinkFullPath); !exists {
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
		if exists, err := util.FileExists(fullExpectedPath); !exists {
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
// Definition of writeModelLayoutForRuntime test cases
//
var adaptModelLayoutTests = []adaptModelLayoutTestCase{
	// Group: file layout / model path support
	{
		ModelID:   "layoutFile",
		ModelType: "unknown",
		ModelPath: "model.data",
		InputFiles: []string{
			"model.data",
			"irrelevant",
		},
		ExpectedLinkPath:   "1/model.data",
		ExpectedLinkTarget: "model.data",
	},
	{
		ModelID:   "layoutDirectoryWithSingleFile",
		ModelType: "unknown",
		ModelPath: "modeldir",
		InputFiles: []string{
			"modeldir/data",
			"irrelevant",
		},
		ExpectedLinkPath:   "1",
		ExpectedLinkTarget: "modeldir",
	},
	{
		ModelID:   "layoutDirectoryWithMultiFile",
		ModelType: "unknown",
		ModelPath: "modeldir",
		InputFiles: []string{
			"modeldir/data",
			"modeldir/file",
			"irrelevant",
		},
		ExpectedLinkPath:   "1",
		ExpectedLinkTarget: "modeldir",
	},
	{
		ModelID:   "layoutDirectoryWithSingleDirectory",
		ModelType: "unknown",
		ModelPath: "modeldir",
		InputFiles: []string{
			"modeldir/subdir/1",
			"modeldir/subdir/2",
			"irrelevant",
		},
		ExpectedLinkPath:   "1",
		ExpectedLinkTarget: "modeldir",
	},
	{
		ModelID:   "layoutDirectoryWithComplex",
		ModelType: "unknown",
		ModelPath: "modeldir",
		InputFiles: []string{
			"modeldir/subdir1/data1",
			"modeldir/subdir2/data2",
			"modeldir/file",
			"irrelevant",
		},
		ExpectedLinkPath:   "1",
		ExpectedLinkTarget: "modeldir",
	},
	{
		ModelID:   "layoutBackwardsCompatibilityComplex",
		ModelType: "unknown",
		ModelPath: "", // points at model id directory
		InputFiles: []string{
			"data/model.1",
			"data/model.2",
			"file",
			"meta.json",
		},
		ExpectedLinkPath:   "1",
		ExpectedLinkTarget: "", // points at model id directory
	},
	{
		ModelID:   "layoutVersionSingleFile",
		ModelType: "unknown",
		ModelPath: "model",
		InputFiles: []string{
			"model/1/file",
			"model/3/file",
			"irrelevant",
		},
		ExpectedLinkPath:   "3",
		ExpectedLinkTarget: "model/3",
	},
	{
		ModelID:   "layoutVersionSingleDir",
		ModelType: "unknown",
		ModelPath: "model",
		InputFiles: []string{
			"model/7/dir/file",
			"model/9/dir/file",
			"irrelevant",
		},
		ExpectedLinkPath:   "9",
		ExpectedLinkTarget: "model/9",
	},
	{
		ModelID:   "layoutVersionMultiFile",
		ModelType: "unknown",
		ModelPath: "model",
		InputFiles: []string{
			"model/1/file",
			"model/2/file1",
			"model/2/file2",
			"irrelevant",
		},
		ExpectedLinkPath:   "2",
		ExpectedLinkTarget: "model/2",
	},
	{
		ModelID:   "layoutVersionMultiFile_2",
		ModelType: "unknown",
		ModelPath: "model/2",
		InputFiles: []string{
			"model/1/file",
			"model/2/file1",
			"model/2/file2",
			"irrelevant",
		},
		ExpectedLinkPath:   "1",
		ExpectedLinkTarget: "model/2",
	},

	// Group: tensorflow
	{
		ModelID:    "tensorflowEmpty",
		ModelType:  "tensorflow",
		InputFiles: []string{
			// no files
		},
		ExpectedLinkPath:   "1/model.savedmodel",
		ExpectedLinkTarget: "",
		ExpectedFiles: []string{
			"1/model.savedmodel",
		},
	},
	{
		ModelID:   "tensorflowSavedModel",
		ModelType: "tensorflow",
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
		ModelID:   "tensorflowGraphDef",
		ModelType: "tensorflow",
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
		ModelID:   "tensorflowGraphDefForFile",
		ModelType: "tensorflow",
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
		ModelID:   "tensorflowSavedModelForDir",
		ModelType: "tensorflow:1.5",
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
		ModelID:   "tensorflowAllowSubdir_1",
		ModelType: "tensorflow",
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
		ModelID:   "tensorflowAllowSubdir_2",
		ModelType: "tensorflow",
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
		ModelID:   "tensorflowVersionDirWithFile",
		ModelType: "tensorflow",
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
		ModelID:   "tensorflowVersionDirWithDir",
		ModelType: "tensorflow",
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
		ModelID:   "tensorflowSubdirInVersionDir",
		ModelType: "tensorflow",
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
		ModelID:   "tensorflowAssumeModelDataIfNotAllVersionNumbers",
		ModelType: "tensorflow",
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
		ModelID:   "tensorflowSelectHighVersionSubdir_1",
		ModelType: "tensorflow",
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
		ModelID:   "tensorflowSelectHighVersionSubdir_2",
		ModelType: "tensorflow",
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
		ModelID:   "tensorflowSelectHighVersionSubdir_3",
		ModelType: "tensorflow",
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
		ModelID:   "tensorflowSavedModelWithConfig",
		ModelType: "tensorflow",
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
		ModelID:   "tensorflowFilesWithConfig",
		ModelType: "tensorflow",
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
		ModelID:   "tensorrtWithVersion",
		ModelType: "tensorRT",
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
		ModelID:   "tensorrtWithVersionRename",
		ModelType: "tensorRT:2x",
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
		ModelID:   "tensorrtSimple",
		ModelType: "tensorRT",
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
		ModelID:   "tensorrtSubdir",
		ModelType: "tensorRT",
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
		ModelID:   "tensorrtSimpleMultifile",
		ModelType: "tensorRT",
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
		ModelID:   "tensorrtSubdirMultifile",
		ModelType: "tensorRT",
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
		ModelID:   "tensorrtUnexpected",
		ModelType: "tensorRT",
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
		ModelID:   "pytorchWithVersion",
		ModelType: "PyTorch",
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
		ModelID:   "pytorchSimpleRename",
		ModelType: "PyTorch",
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
		ModelID:   "onnxWithVersionFile",
		ModelType: "onnx",
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
		ModelID:   "onnxWithVersionDir",
		ModelType: "onnx",
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
		ModelID:   "onnxWithVersionDirMultifile",
		ModelType: "onnx",
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
		ModelID:   "onnxSimpleRename",
		ModelType: "onnx",
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
		ModelID:   "onnxMultifile",
		ModelType: "onnx",
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
		ModelID:   "onnxSimpleSubdir",
		ModelType: "onnx",
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
		ModelID:   "onnxSimpleSubdirMultifile",
		ModelType: "onnx",
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

	// Group: schema
	{
		ModelID:     "schemaOnnxSimpleRename",
		ModelType:   "onnx",
		ModelPath:   "my-model.onnx",
		SchemaPath:  "my-schema.json",
		InputSchema: map[string]interface{}{},
		InputFiles: []string{
			"my-model.onnx",
			"my-schema.json",
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
		ModelID:     "schemaBackwardsCompatiblity",
		ModelType:   "onnx",
		SchemaPath:  "_schema.json",
		InputSchema: map[string]interface{}{},
		InputFiles: []string{
			"my-model.onnx",
			"_schema.json",
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
		ModelID:    "schemaPytorchSimple",
		ModelType:  "pytorch",
		ModelPath:  "my-model.pt",
		SchemaPath: "schema.json",
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
			"schema.json",
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
		ModelID:    "schemaTensorflowSavedModelSimple",
		ModelType:  "tensorflow",
		SchemaPath: "_schema.json",
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
			"_schema.json",
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
		ModelID:    "schemaOverwritesConfig",
		ModelType:  "tensorflow",
		SchemaPath: "_schema.json",
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
			"_schema.json",
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
		ModelID:   "schemaEdgeMaxBatchSize",
		ModelType: "pytorch",
		// schema for model that supports batching
		SchemaPath: "_schema.json",
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
			"_schema.json",
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
		ModelID:   "schemaEdgeMaxBatchSizeError",
		ModelType: "pytorch",
		// schema for model that supports batching, but output is missing batch dim
		SchemaPath: "_schema.json",
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
			"_schema.json",
		},
		ExpectError: true,
	},
}
