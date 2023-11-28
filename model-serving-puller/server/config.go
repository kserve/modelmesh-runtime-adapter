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
	"github.com/go-logr/logr"

	. "github.com/kserve/modelmesh-runtime-adapter/internal/envconfig"
)

// PullerServerConfiguration stores configuration variables for the puller server
type PullerServerConfiguration struct {
	Port                int    // Port to run this puller grpc server
	ModelServerEndpoint string // model server endpoint
}

// GetPullerServerConfigFromEnv creates a new PullerConfiguration populated from environment variables
func GetPullerServerConfigFromEnv(log logr.Logger) *PullerServerConfiguration {
	pullerConfig := new(PullerServerConfiguration)
	pullerConfig.Port = GetEnvInt("PORT", 8084, log)
	pullerConfig.ModelServerEndpoint = GetEnvString("MODEL_SERVER_ENDPOINT", "port:8085")
	return pullerConfig
}
