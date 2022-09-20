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
package util

import (
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
)

const (
	modelTypeJSONKey     string = "model_type"
	schemaPathJSONKey    string = "schema_path"
	diskSizeBytesJSONKey string = "disk_size_bytes"
)

// GetModelType first tries to read the type from the LoadModelRequest.ModelKey json
// If there is an error parsing LoadModelRequest.ModelKey or the type is not found there, this will
// return the LoadModelRequest.ModelType which could possibly be an empty string
func GetModelType(req *mmesh.LoadModelRequest, log logr.Logger) string {
	modelType := req.ModelType
	var modelKey map[string]interface{}
	if err := json.Unmarshal([]byte(req.ModelKey), &modelKey); err != nil {
		log.Info("The model type will fall back to LoadModelRequest.ModelType as LoadModelRequest.ModelKey value is not valid JSON", "LoadModelRequest.ModelType", req.ModelType, "LoadModelRequest.ModelKey", req.ModelKey, "Error", err)
	} else if modelKey[modelTypeJSONKey] == nil {
		log.Info("The model type will fall back to LoadModelRequest.ModelType as LoadModelRequest.ModelKey does not have specified attribute", "LoadModelRequest.ModelType", req.ModelType, "attribute", modelTypeJSONKey)
	} else if modelKeyModelType, ok := modelKey[modelTypeJSONKey].(map[string]interface{}); ok {
		if str, ok := modelKeyModelType["name"].(string); ok {
			modelType = str
		} else {
			log.Info("The model type will fall back to LoadModelRequest.ModelType as LoadModelRequest.ModelKey attribute is not a string.", "LoadModelRequest.ModelType", req.ModelType, "attribute", modelTypeJSONKey, "attribute value", modelKey[modelTypeJSONKey])
		}
	} else if str, ok := modelKey[modelTypeJSONKey].(string); ok {
		modelType = str
	} else {
		log.Info("The model type will fall back to LoadModelRequest.ModelType as LoadModelRequest.ModelKey attribute is not a string or map[string].", "LoadModelRequest.ModelType", req.ModelType, "attribute", modelTypeJSONKey, "attribute value", modelKey[modelTypeJSONKey])
	}
	return modelType
}

// GetSchemaPath extracts the schema path from the ModelKey field
// Will return an error if schema path exists but is in an invalid format
func GetSchemaPath(req *mmesh.LoadModelRequest) (string, error) {
	var modelKey map[string]interface{}
	if parseErr := json.Unmarshal([]byte(req.ModelKey), &modelKey); parseErr != nil {
		return "", fmt.Errorf("Invalid modelKey in LoadModelRequest. ModelKey value '%s' is not valid JSON: %s", req.ModelKey, parseErr)
	}

	schemaPath, ok := modelKey[schemaPathJSONKey].(string)
	if !ok && modelKey[schemaPathJSONKey] != nil {
		return "", fmt.Errorf("Invalid schemaPath in LoadModelRequest, '%s' attribute must have a string value. Found value %v", schemaPathJSONKey, modelKey[schemaPathJSONKey])
	}

	return schemaPath, nil
}

func CalcMemCapacity(reqModelKey string, defaultSize int, multiplier float64, log logr.Logger) uint64 {
	// Try to calculate the model size from the disk size passed in the LoadModelRequest.ModelKey
	// but first set the default to fall back on if we cannot get the disk size.
	size := uint64(defaultSize)
	var modelKey map[string]interface{}
	if err := json.Unmarshal([]byte(reqModelKey), &modelKey); err != nil {
		log.Info("'SizeInBytes' will be defaulted as LoadModelRequest.ModelKey value is not valid JSON", "SizeInBytes", size, "model_key", reqModelKey, "error", err)
	} else {
		if modelKey[diskSizeBytesJSONKey] != nil {
			diskSize, ok := modelKey[diskSizeBytesJSONKey].(float64)
			if ok {
				size = uint64(diskSize * multiplier)
				log.Info("Setting 'SizeInBytes' to a multiple of model disk size", "SizeInBytes", size, "disk_size", diskSize, "multiplier", multiplier)
			} else {
				log.Info("'SizeInBytes' will be defaulted as LoadModelRequest.ModelKey 'disk_size_bytes' value is not a number", "SizeInBytes", size, "model_key", modelKey)
			}
		} else {
			log.Info("'SizeInBytes' will be defaulted as LoadModelRequest.ModelKey did not contain a value for 'disk_size_bytes'", "SizeInBytes", size, "model_key", modelKey)
		}
	}
	return size
}
