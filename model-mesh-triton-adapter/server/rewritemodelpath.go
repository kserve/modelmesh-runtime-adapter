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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"google.golang.org/protobuf/encoding/prototext"

	"github.com/kserve/modelmesh-runtime-adapter/internal/envconfig"
	triton "github.com/kserve/modelmesh-runtime-adapter/internal/proto/triton"
	"github.com/kserve/modelmesh-runtime-adapter/util"
)

// modelTypeToDirNameMapping and modelTypeToFileNameMapping contain constant values which define
// the triton repository structures. The values here reflect the behavior of rewriteModelPath.
// The DirName will be used when a directory is detected, and FileName will be used when a single model file is detected.
// See https://docs.nvidia.com/deeplearning/triton-inference-server/master-user-guide/docs/model_repository.html#framework-model-definition
var modelTypeToDirNameMapping = map[string]string{
	"tensorflow": "model.savedmodel",
	"onnx":       "model.onnx",
}

var modelTypeToBackendMapping = map[string]string{
	"tensorflow": "tensorflow",
	"tensorrt":   "tensorrt",
	"onnx":       "onnxruntime",
	"pytorch":    "pytorch",
}

var modelTypeToFileNameMapping = map[string]string{
	"tensorflow": "model.graphdef",
	"tensorrt":   "model.plan",
	"onnx":       "model.onnx",
	"pytorch":    "model.pt",
}

func rewriteModelPath(rootModelDir, modelID, modelType string, log logr.Logger) error {
	// convert to lower case and remove anything after the :
	modelType = strings.ToLower(strings.Split(modelType, ":")[0])

	tritonModelIDDir, err := util.SecureJoin(rootModelDir, tritonModelSubdir, modelID)
	if err != nil {
		log.Error(err, "Unable to securely join", "rootModelDir", rootModelDir, "tritonModelSubdir", tritonModelSubdir, "modelID", modelID)
		return err
	}
	err = os.RemoveAll(tritonModelIDDir)
	if err != nil {
		log.Info("Ignoring error trying to remove dir", "dir_path", tritonModelIDDir, "error", err)
	}

	sourceModelIDDir, err := util.SecureJoin(rootModelDir, modelID)
	if err != nil {
		log.Error(err, "Unable to securely join", "rootModelDir", rootModelDir, "modelID", modelID)
		return err
	}
	files, err := ioutil.ReadDir(sourceModelIDDir)
	if err != nil {
		return fmt.Errorf("Could not read files in dir %s: %w", sourceModelIDDir, err)
	}

	if isTritonModelRepository(files) {
		err = processTritonRepositoryFiles(files, sourceModelIDDir, tritonModelIDDir, log)
	} else {
		err = createTritonModelRepositoryFromFiles(files, modelType, sourceModelIDDir, tritonModelIDDir, log)
	}
	if err != nil {
		return fmt.Errorf("Error processing model/schema files for model %s: %w", modelID, err)
	}
	return nil
}

// Creates the triton model structure /models/_triton_models/model-id/1/model.X where
// model.X is a file or directory with a name defined by the model type (see modelTypeToDirNameMapping and modelTypeToFileNameMapping).
// Within this path there will be a symlink back to the original /models/model-id directory tree.
func createTritonModelRepositoryFromFiles(inputFiles []os.FileInfo, modelType string, sourceModelIDDir string, tritonModelIDDir string, log logr.Logger) error {
	var sourceModelPath, tritonModelPath string
	var err error
	// copy inputFiles so that we can modify it
	files := inputFiles

	// check if the model dir includes our schema file
	var schemaFileExists bool
	schemaPath, err := util.SecureJoin(sourceModelIDDir, envconfig.ModelSchemaFile)
	if err != nil {
		return fmt.Errorf("Error joining path to schema file: %w", err)
	}
	// if it does exist, remove it from the files list because the schema file is not part of the model's data
	// processing of the schema file will happen after the model data is transformed
	schemaFileExists, files = removeFileFromListOfFileInfo(envconfig.ModelSchemaFile, files)

	// try to find the largest version directory
	largestNum := largestNumberDir(files)
	if largestNum != "" {
		// found a version directory so use it
		var jerr error
		sourceModelPath, jerr = util.SecureJoin(sourceModelIDDir, largestNum)
		if jerr != nil {
			log.Error(jerr, "Unable to securely join", "sourceModelIDDir", sourceModelIDDir, "largestNum", largestNum)
			return jerr
		}
		tritonModelPath, err = util.SecureJoin(tritonModelIDDir, largestNum)
		if err != nil {
			log.Error(err, "Unable to securely join", "tritonModelIDDir", tritonModelIDDir, "largestNum", largestNum)
			return err
		}
		files, err = ioutil.ReadDir(sourceModelPath)
		if err != nil {
			return fmt.Errorf("Could not read files in dir %s: %w", sourceModelIDDir, err)
		}
	} else {
		// did not find a version directory, so create "1" in the target path
		sourceModelPath = sourceModelIDDir
		tritonModelPath, err = util.SecureJoin(tritonModelIDDir, "1")
		if err != nil {
			log.Error(err, "Unable to securely join", "tritonModelIDDir", tritonModelIDDir, "filename", "1")
			return err
		}
	}

	if len(files) == 0 {
		// sourceModelPath is an empty dir, so link it as-is
		tritonModelPath, err = util.SecureJoin(tritonModelPath, modelTypeToDirNameMapping[modelType])
		if err != nil {
			log.Error(err, "Unable to securely join", "tritonModelPath", tritonModelPath, "subpath", modelTypeToDirNameMapping[modelType])
			return err
		}
	} else if len(files) == 1 && !files[0].IsDir() {
		// sourceModelPath contains a single file, link it directly and use file model name for type
		sourceModelPath, err = util.SecureJoin(sourceModelPath, files[0].Name())
		if err != nil {
			log.Error(err, "Unable to securely join", "sourceModelPath", sourceModelPath, "filename", files[0].Name())
			return err
		}
		fileName := modelTypeToFileNameMapping[modelType]
		if fileName == "" {
			// no file model name defined for the type, fall back to the same name as the source
			fileName = files[0].Name()
		}
		tritonModelPath, err = util.SecureJoin(tritonModelPath, fileName)
		if err != nil {
			log.Error(err, "Unable to securely join", "tritonModelPath", tritonModelPath, "filename", fileName)
			return err
		}
	} else if len(files) == 1 && files[0].IsDir() {
		// sourceModelPath contains a single dir, link the dir and use dir name for model type
		sourceModelPath, err = util.SecureJoin(sourceModelPath, files[0].Name())
		if err != nil {
			log.Error(err, "Unable to securely join", "sourceModelPath", sourceModelPath, "filename", files[0].Name())
			return err
		}
		dirName := modelTypeToDirNameMapping[modelType]
		if dirName == "" && modelTypeToFileNameMapping[modelType] == "" {
			// no dir name for model type and no file name for model type, so fall back on the source dir name
			// if there is no dir name for model type but there is a file name, we just live this as an empty
			// string essentially removing the single dir and linking directly to the model file
			dirName = files[0].Name()
		}
		tritonModelPath, err = util.SecureJoin(tritonModelPath, dirName)
		if err != nil {
			log.Error(err, "Unable to securely join", "tritonModelPath", tritonModelPath, "dirName", dirName)
			return err
		}
	} else {
		// sourceModelPath contains multiple files, link to it with the dir name for model type
		tritonModelPath, err = util.SecureJoin(tritonModelPath, modelTypeToDirNameMapping[modelType])
		if err != nil {
			log.Error(err, "Unable to securely join", "tritonModelPath", tritonModelPath, "path", modelTypeToDirNameMapping[modelType])
			return err
		}
	}

	err = os.MkdirAll(filepath.Dir(tritonModelPath), 0755)
	if err != nil {
		return fmt.Errorf("Error creating directories for path %s: %w", tritonModelPath, err)
	}

	err = os.Symlink(sourceModelPath, tritonModelPath)
	if err != nil {
		return fmt.Errorf("Error creating symlink: %w", err)
	}

	m := triton.ModelConfig{}
	m.Backend = modelTypeToBackendMapping[modelType]

	// if _schema.json file exists then update config.pbtxt with schema
	if schemaFileExists {
		sm, errs := convertSchemaToConfig(schemaPath, log)

		if errs != nil {
			return errs
		}

		m.Input = sm.Input
		m.Output = sm.Output
	} else {
		return nil // skip writing config.pbtxt if there is no schema file provided
	}

	// for some level of human readability...
	marshalOpts := prototext.MarshalOptions{
		Multiline: true,
	}

	var pbtxtOut []byte

	if pbtxtOut, err = marshalOpts.Marshal(&m); err != nil {
		log.Error(err, "Unable to marshal config.pbtxt")
		return err
	}

	configFile, err := util.SecureJoin(tritonModelIDDir, tritonRepositoryConfigFilename)
	if err != nil {
		return fmt.Errorf("Error joining path to config file: %w", err)
	}
	if err = ioutil.WriteFile(configFile, pbtxtOut, 0644); err != nil {
		log.Error(err, "Unable to create config.pbtxt")
		return err
	}

	return nil
}

// If we detect the source is already in the triton repository structure
// assume it has the proper structure, but process the config.pbtxt to remove
// the `name` field. All other files are symlinked to their source
func processTritonRepositoryFiles(files []os.FileInfo, sourceModelIDDir string, tritonModelIDDir string, log logr.Logger) error {
	err := os.MkdirAll(tritonModelIDDir, 0755)
	if err != nil {
		return fmt.Errorf("Error creating directories for path %s: %w", tritonModelIDDir, err)
	}

	for _, f := range files {
		var err1 error
		filename := f.Name()
		source, err := util.SecureJoin(sourceModelIDDir, filename)
		if err != nil {
			log.Error(err, "Unable to securely join", "sourceModelIDDir", sourceModelIDDir, "filename", filename)
			return err
		}

		// skip if file being handled is _schema.json
		if filename == envconfig.ModelSchemaFile {
			continue
		}

		// special handling of the config file
		if filename == tritonRepositoryConfigFilename {
			var err2 error
			pbtxt, err2 := ioutil.ReadFile(source)
			if err2 != nil {
				return fmt.Errorf("Error reading config file %s: %w", source, err1)
			}
			processedPbtxt, errs := processModelConfig(pbtxt, log, sourceModelIDDir)

			if errs != nil {
				return errs
			}

			target, jerr := util.SecureJoin(tritonModelIDDir, tritonRepositoryConfigFilename)
			if jerr != nil {
				log.Error(jerr, "Unable to securely join", "tritonModelIDDir", tritonModelIDDir, "tritonRepositoryConfigFilename", tritonRepositoryConfigFilename)
				return jerr
			}
			err2 = ioutil.WriteFile(target, processedPbtxt, f.Mode())
			if err2 != nil {
				return fmt.Errorf("Error writing config file %s: %w", source, err)
			}
			continue
		}

		// symlink all other entries
		link, jerr := util.SecureJoin(tritonModelIDDir, filename)
		if jerr != nil {
			log.Error(jerr, "Unable to securely join", "tritonModelIDDir", tritonModelIDDir, "filename", filename)
			return jerr
		}
		err1 = os.Symlink(source, link)
		if err1 != nil {
			return fmt.Errorf("Error creating symlink to %s: %w", source, err)
		}
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

// Returns true if these the files make up the top level of a triton model repository.
// In other words, the parent of the files is the root of the triton repository (we don't pass the parent path
// because then we'd have to read the filesystem more than once).
// Currently this is using the existence of file 'config.pbtxt' to determine it is a triton repository.
func isTritonModelRepository(files []os.FileInfo) bool {
	for _, f := range files {
		if f.Name() == tritonRepositoryConfigFilename {
			return true
		}
	}
	return false
}

// processModelConfig removes the `name` field from a config.pbtxt file and updates schema if required
//
// If `name` exists, Triton asserts that it matches the model id which is the
// same as the directory in the model repository. Model Mesh generates a model
// id that we must use instead. To work around this, we rely on the fact that,
// if `name` is not specified, Triton sets it the name of the directory.
//
// Ref: https://github.com/triton-inference-server/server/blob/master/docs/model_configuration.md#name-platform-and-backend
func processModelConfig(pbtxtIn []byte, log logr.Logger, sourceModelIDDir string) ([]byte, error) {
	var err error
	// parse the pbtxt into a ModelConfig
	m := triton.ModelConfig{}
	if err = prototext.Unmarshal(pbtxtIn, &m); err != nil {
		log.Error(err, "Unable to unmarshal config.pbtxt")
		// return the input and hope for the best
		return pbtxtIn, nil
	}

	// remove the `name` field
	log.Info("Deleting `name` field from config.pbtxt", "removed_name", m.Name)
	m.Name = ""

	// if sourceModelIDDir passed and _schema.json file exists then update schema to config.pbtxt
	schemaPath, err := util.SecureJoin(sourceModelIDDir, envconfig.ModelSchemaFile)
	if err != nil {
		return pbtxtIn, fmt.Errorf("Unable to join path to schema file: %w", err)
	}
	var schemaFileExists bool
	if schemaFileExists, err = fileExists(schemaPath); err != nil {
		return pbtxtIn, fmt.Errorf("Error determining if schema file exists: %w", err)
	}
	if sourceModelIDDir != "" && schemaFileExists {
		sm, errs := convertSchemaToConfig(schemaPath, log)

		if errs != nil {
			return pbtxtIn, errs
		}

		m.Input = sm.Input
		m.Output = sm.Output
	}

	// for some level of human readability...
	marshalOpts := prototext.MarshalOptions{
		Multiline: true,
	}
	var pbtxtOut []byte
	if pbtxtOut, err = marshalOpts.Marshal(&m); err != nil {
		log.Error(err, "Unable to marshal config.pbtxt")
		// return the input and hope for the best
		return pbtxtIn, nil
	}

	return pbtxtOut, nil
}

// removeFileFromListOfFileInfo
// The input `files` content is modified, the order of elements is changed
func removeFileFromListOfFileInfo(filename string, files []os.FileInfo) (bool, []os.FileInfo) {
	var fileIndex int = -1
	for i, f := range files {
		if f.Name() == filename {
			fileIndex = i
		}
	}
	if fileIndex == -1 {
		return false, files
	}
	// overwrite the entry to be removed with the last entry
	files[fileIndex] = files[len(files)-1]
	// then return a shortend slice
	return true, files[:len(files)-1]

}
