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
package modelschema

import (
	"encoding/json"
	"fmt"
	"os"
)

// Filename for the schema JSON
const ModelSchemaFile string = "_schema.json"

// ModelSchema is a subset of the KFSv2 Model Metadata
// https://github.com/kserve/kserve/blob/master/docs/predict-api/v2/required_api.md#model-metadata
type ModelSchema struct {
	Inputs  []TensorMetadata `json:"inputs,omitempty"`
	Outputs []TensorMetadata `json:"outputs,omitempty"`
}

type TensorMetadata struct {
	Name     string  `json:"name"`
	Datatype string  `json:"datatype"`
	Shape    []int64 `json:"shape"`
}

// Possible values for Datatype
const (
	BYTES  string = "BYTES"
	BOOL   string = "BOOL"
	UINT8  string = "UINT8"
	UINT16 string = "UINT16"
	UINT32 string = "UINT32"
	UINT64 string = "UINT64"
	INT8   string = "INT8"
	INT16  string = "INT16"
	INT32  string = "INT32"
	INT64  string = "INT64"
	FP16   string = "FP16"
	FP32   string = "FP32"
	FP64   string = "FP64"
	// extensions to KFSv2 types
	STRING string = "STRING"
)

func NewFromFile(schemaFilename string) (*ModelSchema, error) {
	jsonBytes, err := os.ReadFile(schemaFilename)
	if err != nil {
		return nil, fmt.Errorf("Unable to read model schema file %s: %w", schemaFilename, err)
	}

	schema := ModelSchema{}
	if err := json.Unmarshal([]byte(jsonBytes), &schema); err != nil {
		return nil, fmt.Errorf("Unable to parse model schema JSON: %w", err)
	}

	return &schema, nil
}
