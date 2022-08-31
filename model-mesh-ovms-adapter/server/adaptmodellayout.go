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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-logr/logr"

	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
)

func adaptModelLayoutForRuntime(ctx context.Context, rootModelDir, modelID, modelType, modelPath, schemaPath string, log logr.Logger) error {
	// convert to lower case and remove anything after the :
	modelType = strings.ToLower(strings.Split(modelType, ":")[0])

	ovmsModelIDDir, err := util.SecureJoin(rootModelDir, modelID)
	if err != nil {
		log.Error(err, "Unable to securely join", "rootModelDir", rootModelDir, "modelID", modelID)
		return err
	}
	// clean up and then create directory where the rewritten model repo will live
	if removeErr := os.RemoveAll(ovmsModelIDDir); removeErr != nil {
		log.Info("Ignoring error trying to remove dir", "Directory", ovmsModelIDDir, "Error", removeErr)
	}
	if mkdirErr := os.MkdirAll(ovmsModelIDDir, 0755); mkdirErr != nil {
		return fmt.Errorf("Error creating directories for path %s: %w", ovmsModelIDDir, mkdirErr)
	}

	modelPathInfo, err := os.Stat(modelPath)
	if err != nil {
		return fmt.Errorf("Error calling stat on model file: %w", err)
	}

	if !modelPathInfo.IsDir() {
		// simple case if ModelPath points to a file
		err = createOvmsModelRepositoryFromPath(modelPath, "1", schemaPath, modelType, ovmsModelIDDir, log)
	} else {
		files, err1 := ioutil.ReadDir(modelPath)
		if err1 != nil {
			return fmt.Errorf("Could not read files in dir %s: %w", modelPath, err1)
		}
		err = createOvmsModelRepositoryFromDirectory(files, modelPath, schemaPath, modelType, ovmsModelIDDir, log)
	}
	if err != nil {
		return fmt.Errorf("Error processing model/schema files for model %s: %w", modelID, err)
	}

	return nil
}

// Creates the ovms model structure /models/_ovms_models/model-id/1/<model files>
// Within this path there will be a symlink back to the original /models/model-id directory tree.
func createOvmsModelRepositoryFromDirectory(files []os.FileInfo, modelPath, schemaPath, modelType, ovmsModelIDDir string, log logr.Logger) error {
	var err error

	// allow the directory to contain version directories
	// try to find the largest version directory
	versionNumber := largestNumberDir(files)
	if versionNumber != "" {
		// found a version directory so step into it
		if modelPath, err = util.SecureJoin(modelPath, versionNumber); err != nil {
			log.Error(err, "Unable to securely join", "modelPath", modelPath, "versionNumber", versionNumber)
			return err
		}
	} else {
		versionNumber = "1"
	}

	return createOvmsModelRepositoryFromPath(modelPath, versionNumber, schemaPath, modelType, ovmsModelIDDir, log)
}

func createOvmsModelRepositoryFromPath(modelPath, versionNumber, schemaPath, modelType, ovmsModelIDDir string, log logr.Logger) error {
	var err error

	modelPathInfo, err := os.Stat(modelPath)
	if err != nil {
		return fmt.Errorf("Error calling stat on %s: %w", modelPath, err)
	}

	linkPathComponents := []string{ovmsModelIDDir, versionNumber}
	if !modelPathInfo.IsDir() {
		// special case to rename the file for an ONNX model
		if modelType == "onnx" {
			linkPathComponents = append(linkPathComponents, onnxModelFilename)
		} else {
			linkPathComponents = append(linkPathComponents, modelPathInfo.Name())
		}
	}

	linkPath, err := util.SecureJoin(linkPathComponents...)
	if err != nil {
		return fmt.Errorf("Error joining link path: %w", err)
	}

	if err = os.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
		return fmt.Errorf("Error creating directories for path %s: %w", linkPath, err)
	}

	if err = os.Symlink(modelPath, linkPath); err != nil {
		return fmt.Errorf("Error creating symlink: %w", err)
	}

	if schemaPath == "" {
		return nil
	}

	return nil
}

// Returns the largest positive int dir as long as all fileInfo dirs are integers (files are ignored).
// If fileInfos is empty or contains any any non-integer dirs, this will return the empty string.
func largestNumberDir(fileInfos []os.FileInfo) string {
	largestInt := 0
	largestDir := ""
	for _, f := range fileInfos {
		if !f.IsDir() {
			continue
		}
		i, err := strconv.Atoi(f.Name())
		if err != nil {
			// must all be numbers
			return ""
		}
		if i > largestInt {
			largestInt = i
			largestDir = f.Name()
		}
	}
	return largestDir
}
