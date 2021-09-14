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

	triton "github.com/kserve/modelmesh-runtime-adapter/internal/proto/triton"
	"google.golang.org/protobuf/encoding/prototext"
)

func writeConfigPbtxt(filename string, modelConfig *triton.ModelConfig) error {
	var err error

	// for some level of human readability...
	marshalOpts := prototext.MarshalOptions{
		Multiline: true,
	}

	var pbtxtOut []byte
	if pbtxtOut, err = marshalOpts.Marshal(modelConfig); err != nil {
		return fmt.Errorf("Unable to marshal config.pbtxt: %w", err)
	}

	if err = ioutil.WriteFile(filename, pbtxtOut, 0644); err != nil {
		return fmt.Errorf("Unable to write config.pbtxt: %w", err)
	}
	return nil
}
