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

// Refer to this proto file for schema and supported data types
// https://github.com/triton-inference-server/server/blob/829d4ba2d007fdb9ae71d064039f96101868b1d6/src/core/model_config.proto

import (
	"encoding/json"
	"io/ioutil"

	"fmt"

	"github.com/go-logr/logr"
	triton "github.com/kserve/modelmesh-runtime-adapter/internal/proto/triton"
)

type Schema struct {
	Inputs  []Tensor `json:"inputs"`
	Outputs []Tensor `json:"outputs"`
}

type Tensor struct {
	Name     string  `json:"name"`
	Datatype string  `json:"datatype,omitempty"`
	Shape    []int64 `json:"shape,omitempty"`
}

var (
	TensorType = map[string]triton.DataType{
		"INVALID": triton.DataType_TYPE_INVALID,
		"BOOL":    triton.DataType_TYPE_BOOL,
		"UINT8":   triton.DataType_TYPE_UINT8,
		"UINT16":  triton.DataType_TYPE_UINT16,
		"UINT32":  triton.DataType_TYPE_UINT32,
		"UINT64":  triton.DataType_TYPE_UINT64,
		"INT8":    triton.DataType_TYPE_INT8,
		"INT16":   triton.DataType_TYPE_INT16,
		"INT32":   triton.DataType_TYPE_INT32,
		"INT64":   triton.DataType_TYPE_INT64,
		"FP16":    triton.DataType_TYPE_FP16,
		"FP32":    triton.DataType_TYPE_FP32,
		"FP64":    triton.DataType_TYPE_FP64,
		"STRING":  triton.DataType_TYPE_STRING,
	}
)

func convertSchemaToConfig(schemaFile string, log logr.Logger) (*triton.ModelConfig, error) {
	config := triton.ModelConfig{}

	schemaInput, err := ioutil.ReadFile(schemaFile)
	if err != nil {
		return nil, fmt.Errorf("Unable to read model schema file %s: %w", schemaFile, err)
	}

	schemaJson := Schema{}
	if err := json.Unmarshal(schemaInput, &schemaJson); err != nil {
		return nil, fmt.Errorf("Unable to parse model schema file %s: %w", schemaFile, err)
	}

	config.Input = make([]*triton.ModelInput, len(schemaJson.Inputs))
	for index, input := range schemaJson.Inputs {
		modelInput := triton.ModelInput{Name: input.Name, Dims: input.Shape, DataType: TensorType[input.Datatype]}
		config.Input[index] = &modelInput
	}

	config.Output = make([]*triton.ModelOutput, len(schemaJson.Outputs))
	for index, output := range schemaJson.Outputs {
		modelOutput := triton.ModelOutput{Name: output.Name, Dims: output.Shape, DataType: TensorType[output.Datatype]}
		config.Output[index] = &modelOutput
	}

	return &config, nil
}
