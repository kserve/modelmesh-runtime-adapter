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

import (
	"testing"

	"github.com/kserve/modelmesh-runtime-adapter/internal/modelschema"
	triton "github.com/kserve/modelmesh-runtime-adapter/internal/proto/triton"
	"google.golang.org/protobuf/proto"
)

func TestConvertSchema(t *testing.T) {
	schema := modelschema.ModelSchema{
		Inputs: []modelschema.TensorMetadata{
			{
				Name:     "inputs",
				Datatype: modelschema.FP32,
				Shape:    []int64{784},
			},
		},
		Outputs: []modelschema.TensorMetadata{
			{
				Name:     "classes",
				Datatype: modelschema.INT64,
				Shape:    []int64{1},
			},
		},
	}

	var err error
	config, err := convertSchemaToConfig(schema, log)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	expectedConfig := &triton.ModelConfig{
		Input: []*triton.ModelInput{
			{
				Name:     "inputs",
				DataType: triton.DataType_TYPE_FP32,
				Dims:     []int64{784},
			},
		},
		Output: []*triton.ModelOutput{
			{
				Name:     "classes",
				DataType: triton.DataType_TYPE_INT64,
				Dims:     []int64{1},
			},
		},
	}
	if !proto.Equal(config, expectedConfig) {
		t.Errorf("config: %s", config)
	}
}
