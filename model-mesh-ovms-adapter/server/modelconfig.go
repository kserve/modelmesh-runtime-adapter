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

// Types defining the structure of the OVMS Multi-Model config file
//
// Doc: https://github.com/openvinotoolkit/model_server/blob/main/docs/multiple_models_mode.md
// JSON Schema: https://github.com/openvinotoolkit/model_server/blob/eab97207fbe9078f83a3f85b468418555b02959a/src/schema.cpp#L28
//
// EXAMPLE:
// {
//   "model_config_list": [
//     {
//       "config": {
//         "name": "model_name1",
//         "base_path": "/models/model1"
//       }
//     },
//     {
//       "config": {
//         "name": "model_name2",
//         "base_path": "/models/model1"
//       }
//     }
//   ]
// }
type OvmsMultiModelRepositoryConfig struct {
	ModelConfigList []OvmsMultiModelConfigListEntry `json:"model_config_list"`
}

type OvmsMultiModelModelConfig struct {
	Name     string `json:"name"`
	BasePath string `json:"base_path"`
}

type OvmsMultiModelConfigListEntry struct {
	Config OvmsMultiModelModelConfig `json:"config"`
}

// Types defining the REST response containing the model config
//
// EXAMPLE:
// {
//   "mnist": {
//     "model_version_status": [
//       {
//         "version": "3",
//         "state": "AVAILABLE",
//         "status": {
//           "error_code": "OK",
//           "error_message": "OK"
//         }
//       }
//     ]
//   }
// }
//
// Possible states and error codes are defined in: https://github.com/openvinotoolkit/model_server/blob/211b6b759796a5afaf3c9e115f98b4ee890f02e2/src/modelversionstatus.hpp

type OvmsConfigResponse map[string]OvmsModelStatusResponse

type OvmsModelStatusResponse struct {
	ModelVersionStatus []OvmsModelVersionStatus `json:"model_version_status"`
}

type OvmsModelStatus struct {
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

type OvmsModelVersionStatus struct {
	Version string          `json:"version"`
	State   string          `json:"state"`
	Status  OvmsModelStatus `json:"status"`
}

type OvmsConfigErrorResponse struct {
	Error string `json:"error"`
}
