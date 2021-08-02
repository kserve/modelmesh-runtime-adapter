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
	adapterPort                       string = "ADAPTER_PORT"
	defaultAdapterPort                       = 8085
	runtimePort                       string = "RUNTIME_PORT"
	defaultRuntimePort                       = 8001
	tritonContainerMemReqBytes        string = "CONTAINER_MEM_REQ_BYTES"
	defaultTritonContainerMemReqBytes        = -1
	tritonMemBufferBytes              string = "MEM_BUFFER_BYTES"
	defaultTritonMemBufferBytes              = 256 * 1024 * 1024 // 256MB
	maxLoadingConcurrency             string = "LOADING_CONCURRENCY"
	defaultMaxLoadingConcurrency             = 1
	maxLoadingTimeoutMs               string = "LOADTIME_TIMEOUT"
	defaultMaxLoadingTimeoutMs               = 30000
	defaultModelSize                  string = "DEFAULT_MODELSIZE"
	defaultModelSizeInBytes                  = 1000000
	modelSizeMultiplier               string = "MODELSIZE_MULTIPLIER"
	defaultModelSizeMultiplier               = 1.25
	runtimeVersion                    string = "RUNTIME_VERSION"
	defaultRuntimeVersion                    = "v1"
	disableModelConcurrency           string = "DISABLE_MODEL_CONCURRENCY"
	defaulDisableModelConcurrency            = false
	rootModelDir                      string = "ROOT_MODEL_DIR"
	defaultRootModelDir                      = "/models"
	useEmbeddedPuller                 string = "USE_EMBEDDED_PULLER"
	defaultUseEmbeddedPuller                 = false
)

func GetAdapterConfigurationFromEnv(log logr.Logger) (*AdapterConfiguration, error) {
	adapterConfig := new(AdapterConfiguration)
	adapterConfig.Port = GetEnvInt(adapterPort, defaultAdapterPort, log)
	adapterConfig.TritonPort = GetEnvInt(runtimePort, defaultRuntimePort, log)
	adapterConfig.TritonContainerMemReqBytes = GetEnvInt(tritonContainerMemReqBytes, defaultTritonContainerMemReqBytes, log)
	adapterConfig.TritonMemBufferBytes = GetEnvInt(tritonMemBufferBytes, defaultTritonMemBufferBytes, log)
	adapterConfig.CapacityInBytes = adapterConfig.TritonContainerMemReqBytes - adapterConfig.TritonMemBufferBytes
	adapterConfig.MaxLoadingConcurrency = GetEnvInt(maxLoadingConcurrency, defaultMaxLoadingConcurrency, log)
	adapterConfig.ModelLoadingTimeoutMS = GetEnvInt(maxLoadingTimeoutMs, defaultMaxLoadingTimeoutMs, log)
	adapterConfig.DefaultModelSizeInBytes = GetEnvInt(defaultModelSize, defaultModelSizeInBytes, log)
	adapterConfig.ModelSizeMultiplier = GetEnvFloat(modelSizeMultiplier, defaultModelSizeMultiplier, log)
	adapterConfig.RuntimeVersion = GetEnvString(runtimeVersion, defaultRuntimeVersion)                            // retrieved from runtime status later
	adapterConfig.LimitModelConcurrency = GetEnvBool(disableModelConcurrency, defaulDisableModelConcurrency, log) // enabled by default
	adapterConfig.RootModelDir = GetEnvString(rootModelDir, defaultRootModelDir)
	adapterConfig.UseEmbeddedPuller = GetEnvBool(useEmbeddedPuller, defaultUseEmbeddedPuller, log)

	if adapterConfig.TritonContainerMemReqBytes < 0 {
		return adapterConfig, fmt.Errorf("%s environment variable must be set to a positive integer, found value %v", tritonContainerMemReqBytes, adapterConfig.TritonContainerMemReqBytes)
	}
	if adapterConfig.ModelSizeMultiplier <= 0 {
		return adapterConfig, fmt.Errorf("%s environment variable must be greater than 0, found value %v", modelSizeMultiplier, adapterConfig.ModelSizeMultiplier)
	}
	return adapterConfig, nil
}
