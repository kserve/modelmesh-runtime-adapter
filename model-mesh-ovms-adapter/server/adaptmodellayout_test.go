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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
)

type adaptModelLayoutTestCase struct {
	ModelID            string
	ModelType          string
	ModelPath          string
	SchemaPath         string
	InputFiles         []string
	InputSchema        map[string]interface{}
	ExpectedLinkPath   string
	ExpectedLinkTarget string
	ExpectedFiles      []string
	ExpectError        bool
}

func (tt adaptModelLayoutTestCase) getSourceDir() string {
	return filepath.Join(generatedTestdataDir, tt.ModelID)
}

func (tt adaptModelLayoutTestCase) getTargetDir() string {
	return filepath.Join(ovmsModelsDir, tt.ModelID)
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
	ovmsRootModelDir := filepath.Join(generatedTestdataDir, ovmsModelSubdir)
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
			err = adaptModelLayoutForRuntime(context.Background(), ovmsRootModelDir, tt.ModelID, tt.ModelType, modelFullPath, schemaFullPath, log)

			if tt.ExpectError && err == nil {
				t.Fatal("ExpectError is true, but no error was returned")
			}

			if !tt.ExpectError && err != nil {
				t.Fatal("Did not expect the error", err)
			}

			assertLinkAndPathsExist(t, tt)
			// assertConfigFileContents(t, tt)
		})
	}
}

func TestAdaptModelLayoutForRuntime_Multiple(t *testing.T) {
	// cleanup the entire generated testdata dir now instead of before each
	// test case so we make sure the function works with existing models
	// The test setup will recreate the directory
	err := os.RemoveAll(generatedTestdataDir)
	if err != nil {
		t.Fatalf("Could not remove root model dir %s due to error %v", generatedTestdataDir, err)
	}
	ovmsRootModelDir := filepath.Join(generatedTestdataDir, ovmsModelSubdir)

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
		err = adaptModelLayoutForRuntime(ctx, ovmsRootModelDir, tt.ModelID, tt.ModelType, modelFullPath, schemaFullPath, log)
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
		// assertConfigFileContents(t, tt)
	}
}

//
// Helper functions
//

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
	// Group: ONNX format
	{
		ModelID:   "onnxSimple",
		ModelType: "onnx",
		ModelPath: "model.onnx",
		InputFiles: []string{
			"model.onnx",
		},
		ExpectedLinkPath:   "1/model.onnx",
		ExpectedLinkTarget: "model.onnx",
	},
	{
		ModelID:   "onnxRename",
		ModelType: "onnx",
		ModelPath: "mymodel.data",
		InputFiles: []string{
			"mymodel.data",
			"irrelevant",
		},
		ExpectedLinkPath:   "1/model.onnx",
		ExpectedLinkTarget: "mymodel.data",
	},
	{
		ModelID:   "onnxNativeLayout",
		ModelType: "onnx",
		ModelPath: "my_model",
		InputFiles: []string{
			"my_model/2/model.onnx",
			"my_model/6/model.onnx",
		},
		ExpectedLinkPath:   "6",
		ExpectedLinkTarget: "my_model/6",
	},

	// Group: OpenVINO IR format
	{
		ModelID:   "openvinoSimple",
		ModelType: "openvino",
		ModelPath: "my_model",
		InputFiles: []string{
			"my_model/model.bin",
			"my_model/model.xml",
		},
		ExpectedLinkPath:   "1",
		ExpectedLinkTarget: "my_model",
	},
	{
		ModelID:   "openvinoNativeLayout",
		ModelType: "openvino",
		ModelPath: "my_model",
		InputFiles: []string{
			"my_model/2/model.bin",
			"my_model/2/model.xml",
			"my_model/5/model.bin",
			"my_model/5/model.xml",
		},
		ExpectedLinkPath:   "5",
		ExpectedLinkTarget: "my_model/5",
	},

	// Group: General
	{
		ModelID:   "pathToFile",
		ModelType: "general",
		ModelPath: "my_model",
		InputFiles: []string{
			"my_model",
		},
		ExpectedLinkPath:   "1/my_model",
		ExpectedLinkTarget: "my_model",
	},
	{
		ModelID:   "pathToDir",
		ModelType: "general",
		ModelPath: "dir",
		InputFiles: []string{
			"dir/model_file_1",
			"dir/model_file_2",
			"dir/model_file_3",
		},
		ExpectedLinkPath:   "1",
		ExpectedLinkTarget: "dir",
	},
	{
		ModelID:   "versionDir",
		ModelType: "general",
		ModelPath: "2",
		InputFiles: []string{
			"2/model_file_1",
			"2/model_file_2",
			"2/model_file_3",
		},
		ExpectedLinkPath:   "1",
		ExpectedLinkTarget: "2",
	},
}
