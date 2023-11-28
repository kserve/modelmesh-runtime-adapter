// Copyright 2021 IBM Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package server

// Refer to this proto file for schema and supported data types
// https://github.com/triton-inference-server/server/blob/829d4ba2d007fdb9ae71d064039f96101868b1d6/src/core/model_config.proto

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/kserve/modelmesh-runtime-adapter/internal/modelschema"
	triton "github.com/kserve/modelmesh-runtime-adapter/internal/proto/triton"
)

var (
	TensorType = map[string]triton.DataType{
		"INVALID":          triton.DataType_TYPE_INVALID,
		modelschema.BOOL:   triton.DataType_TYPE_BOOL,
		modelschema.UINT8:  triton.DataType_TYPE_UINT8,
		modelschema.UINT16: triton.DataType_TYPE_UINT16,
		modelschema.UINT32: triton.DataType_TYPE_UINT32,
		modelschema.UINT64: triton.DataType_TYPE_UINT64,
		modelschema.INT8:   triton.DataType_TYPE_INT8,
		modelschema.INT16:  triton.DataType_TYPE_INT16,
		modelschema.INT32:  triton.DataType_TYPE_INT32,
		modelschema.INT64:  triton.DataType_TYPE_INT64,
		modelschema.FP16:   triton.DataType_TYPE_FP16,
		modelschema.FP32:   triton.DataType_TYPE_FP32,
		modelschema.FP64:   triton.DataType_TYPE_FP64,
		modelschema.STRING: triton.DataType_TYPE_STRING,
	}
)

func convertSchemaToConfigFromFile(schemaFile string, log logr.Logger) (*triton.ModelConfig, error) {
	ms, err := modelschema.NewFromFile(schemaFile)
	if err != nil {
		return nil, fmt.Errorf("Error trying to convert schema to config: %w", err)
	}

	return convertSchemaToConfig(*ms, log)
}

func convertSchemaToConfig(schema modelschema.ModelSchema, log logr.Logger) (*triton.ModelConfig, error) {
	config := triton.ModelConfig{}

	if schema.Inputs != nil {
		config.Input = make([]*triton.ModelInput, len(schema.Inputs))
		for index, input := range schema.Inputs {
			modelInput := triton.ModelInput{Name: input.Name, Dims: input.Shape, DataType: TensorType[input.Datatype]}
			config.Input[index] = &modelInput
		}
	}

	if schema.Outputs != nil {
		config.Output = make([]*triton.ModelOutput, len(schema.Outputs))
		for index, output := range schema.Outputs {
			modelOutput := triton.ModelOutput{Name: output.Name, Dims: output.Shape, DataType: TensorType[output.Datatype]}
			config.Output[index] = &modelOutput
		}
	}

	return &config, nil
}
