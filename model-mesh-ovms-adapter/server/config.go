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

	"github.com/go-logr/logr"

	. "github.com/kserve/modelmesh-runtime-adapter/internal/envconfig"
)

const (
	adapterPort                         string = "ADAPTER_PORT"
	defaultAdapterPort                         = 8085
	runtimePort                         string = "RUNTIME_PORT"
	defaultRuntimePort                         = 8001
	openvinoContainerMemReqBytes        string = "CONTAINER_MEM_REQ_BYTES"
	defaultOpenvinoContainerMemReqBytes        = -1
	openvinoMemBufferBytes              string = "MEM_BUFFER_BYTES"
	defaultOpenvinoMemBufferBytes              = 256 * 1024 * 1024 // 256MB
	maxLoadingConcurrency               string = "LOADING_CONCURRENCY"
	defaultMaxLoadingConcurrency               = 1
	maxLoadingTimeoutMs                 string = "LOADTIME_TIMEOUT"
	defaultMaxLoadingTimeoutMs                 = 30000
	defaultModelSize                    string = "DEFAULT_MODELSIZE"
	defaultModelSizeInBytes                    = 1000000
	modelSizeMultiplier                 string = "MODELSIZE_MULTIPLIER"
	defaultModelSizeMultiplier                 = 1.25
	runtimeVersion                      string = "RUNTIME_VERSION"
	defaultRuntimeVersion                      = "TODO"
	limitPerModelConcurrency            string = "LIMIT_PER_MODEL_CONCURRENCY"
	defaultLimitPerModelConcurrency            = 0 // 0 means don't limit request concurrency
	rootModelDir                        string = "ROOT_MODEL_DIR"
	defaultRootModelDir                        = "/models"
	modelConfigFile                     string = "MODEL_CONFIG_FILE"
	defaultModelConfigFile                     = "/models/model_config_list.json"
	useEmbeddedPuller                   string = "USE_EMBEDDED_PULLER"
	defaultUseEmbeddedPuller                   = false
)

func GetAdapterConfigurationFromEnv(log logr.Logger) (*AdapterConfiguration, error) {
	adapterConfig := new(AdapterConfiguration)
	adapterConfig.Port = GetEnvInt(adapterPort, defaultAdapterPort, log)
	adapterConfig.OpenVinoPort = GetEnvInt(runtimePort, defaultRuntimePort, log)
	adapterConfig.OpenvinoContainerMemReqBytes = GetEnvInt(openvinoContainerMemReqBytes, defaultOpenvinoContainerMemReqBytes, log)
	adapterConfig.OpenvinoMemBufferBytes = GetEnvInt(openvinoMemBufferBytes, defaultOpenvinoMemBufferBytes, log)
	adapterConfig.CapacityInBytes = adapterConfig.OpenvinoContainerMemReqBytes - adapterConfig.OpenvinoMemBufferBytes
	adapterConfig.MaxLoadingConcurrency = GetEnvInt(maxLoadingConcurrency, defaultMaxLoadingConcurrency, log)
	adapterConfig.ModelLoadingTimeoutMS = GetEnvInt(maxLoadingTimeoutMs, defaultMaxLoadingTimeoutMs, log)
	adapterConfig.DefaultModelSizeInBytes = GetEnvInt(defaultModelSize, defaultModelSizeInBytes, log)
	adapterConfig.ModelSizeMultiplier = GetEnvFloat(modelSizeMultiplier, defaultModelSizeMultiplier, log)
	adapterConfig.RuntimeVersion = GetEnvString(runtimeVersion, defaultRuntimeVersion)
	adapterConfig.LimitModelConcurrency = GetEnvInt(limitPerModelConcurrency, defaultLimitPerModelConcurrency, log)
	adapterConfig.RootModelDir = GetEnvString(rootModelDir, defaultRootModelDir)
	adapterConfig.ModelConfigFile = GetEnvString(modelConfigFile, defaultModelConfigFile)
	adapterConfig.UseEmbeddedPuller = GetEnvBool(useEmbeddedPuller, defaultUseEmbeddedPuller, log)

	if adapterConfig.OpenvinoContainerMemReqBytes < 0 {
		return adapterConfig, fmt.Errorf("%s environment variable must be set to a positive integer, found value %v", openvinoContainerMemReqBytes, adapterConfig.OpenvinoContainerMemReqBytes)
	}
	if adapterConfig.ModelSizeMultiplier <= 0 {
		return adapterConfig, fmt.Errorf("%s environment variable must be greater than 0, found value %v", modelSizeMultiplier, adapterConfig.ModelSizeMultiplier)
	}
	return adapterConfig, nil
}