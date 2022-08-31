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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"google.golang.org/protobuf/encoding/prototext"

	"github.com/kserve/modelmesh-runtime-adapter/internal/modelschema"
	triton "github.com/kserve/modelmesh-runtime-adapter/internal/proto/triton"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
)

// modelTypeToDirNameMapping and modelTypeToFileNameMapping contain constant values which define
// the triton repository structures. The values here reflect the behavior of writeModelLayoutForRuntime.
// The DirName will be used when a directory is detected, and FileName will be used when a single model file is detected.
// See https://docs.nvidia.com/deeplearning/triton-inference-server/master-user-guide/docs/model_repository.html#framework-model-definition
var modelTypeToDirNameMapping = map[string]string{
	"tensorflow": tensorflowSavedModelDirName,
	"onnx":       "model.onnx",
	"keras":      "model.savedmodel",
}

var modelTypeToBackendMapping = map[string]string{
	"tensorflow": "tensorflow",
	"tensorrt":   "tensorrt",
	"onnx":       "onnxruntime",
	"pytorch":    "pytorch",
	"keras":      "tensorflow",
}

var modelTypeToFileNameMapping = map[string]string{
	"tensorflow": "model.graphdef",
	"tensorrt":   "model.plan",
	"onnx":       "model.onnx",
	"pytorch":    "model.pt",
}

func adaptModelLayoutForRuntime(ctx context.Context, rootModelDir, modelID, modelType, modelPath, schemaPath string, log logr.Logger) error {
	// convert to lower case and remove anything after the :
	modelType = strings.ToLower(strings.Split(modelType, ":")[0])

	tritonModelIDDir, err := util.SecureJoin(rootModelDir, modelID)
	if err != nil {
		log.Error(err, "Unable to securely join", "rootModelDir", rootModelDir, "modelID", modelID)
		return err
	}
	// clean up and then create directory where the rewritten model repo will live
	if removeErr := os.RemoveAll(tritonModelIDDir); removeErr != nil {
		log.Info("Ignoring error trying to remove dir", "Directory", tritonModelIDDir, "Error", removeErr)
	}
	if mkdirErr := os.MkdirAll(tritonModelIDDir, 0755); mkdirErr != nil {
		return fmt.Errorf("Error creating directories for path %s: %w", tritonModelIDDir, mkdirErr)
	}

	modelPathInfo, err := os.Stat(modelPath)
	if err != nil {
		return fmt.Errorf("Error calling stat on model file: %w", err)
	}

	// maybe convert from Keras to TF
	if !modelPathInfo.IsDir() && modelType == "keras" {
		// place the converted file next to the .h5 file
		modelDir := filepath.Dir(modelPath)
		convertedFilename, err1 := util.SecureJoin(modelDir, tensorflowSavedModelDirName)
		if err1 != nil {
			log.Error(err1, "Unable to securely join", "elem1", modelDir, "elem2", tensorflowSavedModelDirName)
			return err1
		}

		if err1 = convertKerasToTF(modelPath, convertedFilename, ctx, log); err1 != nil {
			return fmt.Errorf("Error while converting keras model %s to tensorflow: %w", modelPath, err1)
		}

		// reassign model path to the converted file
		modelPath = convertedFilename
	}

	if !modelPathInfo.IsDir() {
		// simple case if ModelPath points to a file
		err = createTritonModelRepositoryFromPath(modelPath, "1", schemaPath, modelType, tritonModelIDDir, log)
	} else {
		files, err1 := ioutil.ReadDir(modelPath)
		if err1 != nil {
			return fmt.Errorf("Could not read files in dir %s: %w", modelPath, err1)
		}

		if isTritonModelRepository(files) {
			err = adaptNativeModelLayout(files, modelPath, schemaPath, tritonModelIDDir, log)
		} else {
			err = createTritonModelRepositoryFromDirectory(files, modelPath, schemaPath, modelType, tritonModelIDDir, log)
		}
	}
	if err != nil {
		return fmt.Errorf("Error processing model/schema files for model %s: %w", modelID, err)
	}

	return nil
}

// Creates the triton model structure /models/_triton_models/model-id/1/model.X where
// model.X is a file or directory with a name defined by the model type (see modelTypeToDirNameMapping and modelTypeToFileNameMapping).
// Within this path there will be a symlink back to the original /models/model-id directory tree.
func createTritonModelRepositoryFromDirectory(files []os.FileInfo, modelPath, schemaPath, modelType, tritonModelIDDir string, log logr.Logger) error {
	var err error

	// for backwards compatibility, remove any file called _schema.json from
	// the set of files before processing
	_, files = util.RemoveFileFromListOfFileInfo(modelschema.ModelSchemaFile, files)

	// allow the directory to contain version directories
	// try to find the largest version directory
	versionNumber := largestNumberDir(files)
	if versionNumber != "" {
		// found a version directory so step into it
		if modelPath, err = util.SecureJoin(modelPath, versionNumber); err != nil {
			log.Error(err, "Unable to securely join", "modelPath", modelPath, "versionNumber", versionNumber)
			return err
		}

		if files, err = ioutil.ReadDir(modelPath); err != nil {
			return fmt.Errorf("Could not read files in dir %s: %w", modelPath, err)
		}
	} else {
		versionNumber = "1"
	}

	// for backwards compatibility, special handling for known model types
	// with a directory with a single entry
	if len(files) == 1 {
		_, okd := modelTypeToDirNameMapping[modelType]
		_, okf := modelTypeToFileNameMapping[modelType]
		if okd || okf {
			var jerr error
			modelPath, jerr = util.SecureJoin(modelPath, files[0].Name())
			if jerr != nil {
				log.Error(jerr, "Unable to securely join", "modelPath", modelPath, "versionNumber", versionNumber)
				return jerr
			}
		}
	}

	return createTritonModelRepositoryFromPath(modelPath, versionNumber, schemaPath, modelType, tritonModelIDDir, log)
}

func createTritonModelRepositoryFromPath(modelPath, versionNumber, schemaPath, modelType, tritonModelIDDir string, log logr.Logger) error {
	var err error

	modelPathInfo, err := os.Stat(modelPath)
	if err != nil {
		return fmt.Errorf("Error calling stat on %s: %w", modelPath, err)
	}

	var linkPath string
	if modelPathInfo.IsDir() {
		// if there is a known directory name for the model type, use
		// it, otherwise use the directory's contents as the model data
		if dirName, ok := modelTypeToDirNameMapping[modelType]; ok {
			if linkPath, err = util.SecureJoin(versionNumber, dirName); err != nil {
				return fmt.Errorf("Error joining link path: %w", err)
			}
		} else {
			linkPath = versionNumber
		}
	} else {
		if name, ok := modelTypeToFileNameMapping[modelType]; ok {
			if linkPath, err = util.SecureJoin(versionNumber, name); err != nil {
				return fmt.Errorf("Error joining link path: %w", err)
			}
		} else if linkPath, err = util.SecureJoin(versionNumber, filepath.Base(modelPath)); err != nil {
			return fmt.Errorf("Error joining link path: %w", err)
		}
	}
	if linkPath, err = util.SecureJoin(tritonModelIDDir, linkPath); err != nil {
		return fmt.Errorf("Error joining link path: %w", err)
	}

	if err = os.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
		return fmt.Errorf("Error creating directories for path %s: %w", linkPath, err)
	}

	if err = os.Symlink(modelPath, linkPath); err != nil {
		return fmt.Errorf("Error creating symlink: %w", err)
	}

	// if schemaPath doesn't exist don't write out a config.pbtxt
	if schemaPath == "" {
		return nil
	}

	sm, err := convertSchemaToConfigFromFile(schemaPath, log)
	if err != nil {
		return err
	}

	m := triton.ModelConfig{
		Backend: modelTypeToBackendMapping[modelType],
		Input:   sm.Input,
		Output:  sm.Output,
	}

	configFile, err := util.SecureJoin(tritonModelIDDir, tritonRepositoryConfigFilename)
	if err != nil {
		return fmt.Errorf("Error joining path to config file: %w", err)
	}
	if err = writeConfigPbtxt(configFile, &m); err != nil {
		return err
	}

	return nil
}

// If the Triton specific config file exists, assume the model files has the
// proper structure, but process the config.pbtxt to remove the `name` field.
// All other files are symlinked to their source
func adaptNativeModelLayout(files []os.FileInfo, sourceModelIDDir, schemaPath, tritonModelIDDir string, log logr.Logger) error {
	for _, f := range files {
		var err1 error
		filename := f.Name()
		source, err1 := util.SecureJoin(sourceModelIDDir, filename)
		if err1 != nil {
			log.Error(err1, "Unable to securely join", "sourceModelIDDir", sourceModelIDDir, "filename", filename)
			return err1
		}

		// special handling of the config file
		if filename == tritonRepositoryConfigFilename {
			pbtxt, err2 := ioutil.ReadFile(source)
			if err2 != nil {
				return fmt.Errorf("Error reading config file %s: %w", source, err2)
			}

			processedPbtxt, err2 := processModelConfig(pbtxt, schemaPath, log)
			if err2 != nil {
				return err2
			}

			target, err2 := util.SecureJoin(tritonModelIDDir, tritonRepositoryConfigFilename)
			if err2 != nil {
				log.Error(err2, "Unable to securely join", "tritonModelIDDir", tritonModelIDDir, "tritonRepositoryConfigFilename", tritonRepositoryConfigFilename)
				return err2
			}

			if err2 = ioutil.WriteFile(target, processedPbtxt, f.Mode()); err2 != nil {
				return fmt.Errorf("Error writing config file %s: %w", source, err2)
			}

			continue
		}

		// symlink all other entries
		link, jerr := util.SecureJoin(tritonModelIDDir, filename)
		if jerr != nil {
			log.Error(jerr, "Unable to securely join", "tritonModelIDDir", tritonModelIDDir, "filename", filename)
			return jerr
		}
		if err1 = os.Symlink(source, link); err1 != nil {
			return fmt.Errorf("Error creating symlink to %s: %w", source, err1)
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
func processModelConfig(pbtxtIn []byte, schemaPath string, log logr.Logger) ([]byte, error) {
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

	if schemaPath != "" {
		sm, errs := convertSchemaToConfigFromFile(schemaPath, log)

		if errs != nil {
			return pbtxtIn, errs
		}

		// handle batch support
		//
		// In Triton, if max_batch_size > 0 the input and output schema
		// are modified to include the batch dimension as [ -1 ] + dims
		// REF: https://github.com/triton-inference-server/server/blob/main/docs/model_configuration.md#maximum-batch-size
		// In Serving, we do not have config for max_batch_size in the
		// schema, so we require the batch dimension to be explicit. If
		// the schema file is provided and a model config has
		// max_batch_size > 0, the added dims need to be removed before
		// writing the config.pbtxt
		if m.MaxBatchSize > 0 {
			if !allInputsAndOuputsHaveBatchDimension(sm) {
				return pbtxtIn, errors.New("Conflicting model configuration: If model has schema and config.pbtxt with max_batch_size > 0, then the first dimension of all inputs and outputs must have size -1.")
			}
			removeFirstDimensionFromInputsAndOutputs(sm)
		}

		// rewrite schema input/output
		if sm.Input != nil {
			m.Input = sm.Input
		}
		if sm.Output != nil {
			m.Output = sm.Output
		}
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

func allInputsAndOuputsHaveBatchDimension(m *triton.ModelConfig) bool {
	for _, in := range m.Input {
		if in.Dims[0] != -1 {
			return false
		}
	}
	for _, out := range m.Output {
		if out.Dims[0] != -1 {
			return false
		}
	}
	return true
}

func removeFirstDimensionFromInputsAndOutputs(m *triton.ModelConfig) {
	for _, in := range m.Input {
		in.Dims = in.Dims[1:]
	}
	for _, out := range m.Output {
		out.Dims = out.Dims[1:]
	}
}
