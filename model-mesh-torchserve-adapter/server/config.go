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

	"github.com/kserve/modelmesh-runtime-adapter/internal/util"

	"github.com/go-logr/logr"

	. "github.com/kserve/modelmesh-runtime-adapter/internal/envconfig"
)

const (
	adapterPort                           string = "ADAPTER_PORT"
	defaultAdapterPort                           = 8085
	runtimePort                           string = "RUNTIME_PORT"
	defaultRuntimePort                           = 7071
	runtimeDataEndpoint                   string = "RUNTIME_DATA_ENDPOINT"
	defaultRuntimeDataEndpoint                   = "port:7070"
	torchServeContainerMemReqBytes        string = "CONTAINER_MEM_REQ_BYTES"
	defaultTorchServeContainerMemReqBytes        = -1
	torchServeMemBufferBytes              string = "MEM_BUFFER_BYTES"
	defaultTorchServeMemBufferBytes              = 256 * 1024 * 1024 // 256MB
	maxLoadingConcurrency                 string = "LOADING_CONCURRENCY"
	defaultMaxLoadingConcurrency                 = 1
	maxLoadingTimeoutMs                   string = "LOADTIME_TIMEOUT"
	defaultMaxLoadingTimeoutMs                   = 30000
	defaultModelSize                      string = "DEFAULT_MODELSIZE"
	defaultModelSizeInBytes                      = 1000000
	modelSizeMultiplier                   string = "MODELSIZE_MULTIPLIER"
	defaultModelSizeMultiplier                   = 2.75
	runtimeVersion                        string = "RUNTIME_VERSION"
	defaultRuntimeVersion                        = "v1"
	limitPerModelConcurrency              string = "LIMIT_PER_MODEL_CONCURRENCY"
	defaultLimitPerModelConcurrency              = 0 // 0 means don't limit request concurrency
	rootModelDir                          string = "ROOT_MODEL_DIR"
	defaultRootModelDir                          = "/models"
	torchServeModelStoreDirName           string = "_torchserve_models"
	useEmbeddedPuller                     string = "USE_EMBEDDED_PULLER"
	defaultUseEmbeddedPuller                     = false

	// TorchServe specific
	requestBatchSize         string = "REQUEST_BATCH_SIZE"
	defaultRequestBatchSize         = 0
	maxBatchDelaySecs        string = "MAX_BATCH_DELAY_SECS"
	defaultMaxBatchDelaySecs        = 0
)

func GetAdapterConfigurationFromEnv(log logr.Logger) (*AdapterConfiguration, error) {
	adapterConfig := new(AdapterConfiguration)
	adapterConfig.Port = GetEnvInt(adapterPort, defaultAdapterPort, log)
	adapterConfig.TorchServeManagementPort = GetEnvInt(runtimePort, defaultRuntimePort, log)
	adapterConfig.TorchServeInferenceEndpoint = GetEnvString(runtimeDataEndpoint, defaultRuntimeDataEndpoint)
	adapterConfig.TorchServeContainerMemReqBytes = GetEnvInt(torchServeContainerMemReqBytes, defaultTorchServeContainerMemReqBytes, log)
	adapterConfig.TorchServeMemBufferBytes = GetEnvInt(torchServeMemBufferBytes, defaultTorchServeMemBufferBytes, log)
	adapterConfig.CapacityInBytes = adapterConfig.TorchServeContainerMemReqBytes - adapterConfig.TorchServeMemBufferBytes
	adapterConfig.MaxLoadingConcurrency = GetEnvInt(maxLoadingConcurrency, defaultMaxLoadingConcurrency, log)
	adapterConfig.ModelLoadingTimeoutMS = GetEnvInt(maxLoadingTimeoutMs, defaultMaxLoadingTimeoutMs, log)
	adapterConfig.DefaultModelSizeInBytes = GetEnvInt(defaultModelSize, defaultModelSizeInBytes, log)
	adapterConfig.ModelSizeMultiplier = GetEnvFloat(modelSizeMultiplier, defaultModelSizeMultiplier, log)
	adapterConfig.RuntimeVersion = GetEnvString(runtimeVersion, defaultRuntimeVersion)
	adapterConfig.LimitModelConcurrency = GetEnvInt(limitPerModelConcurrency, defaultLimitPerModelConcurrency, log)
	adapterConfig.UseEmbeddedPuller = GetEnvBool(useEmbeddedPuller, defaultUseEmbeddedPuller, log)

	var err error
	adapterConfig.ModelStoreDir, err = util.SecureJoin(GetEnvString(rootModelDir, defaultRootModelDir), torchServeModelStoreDirName)
	if err != nil {
		return nil, fmt.Errorf("Could not construct torchserve model store path: %w", err)
	}

	adapterConfig.RequestBatchSize = int32(GetEnvInt(requestBatchSize, defaultRequestBatchSize, log))
	adapterConfig.MaxBatchDelaySecs = int32(GetEnvInt(maxBatchDelaySecs, defaultMaxBatchDelaySecs, log))

	if adapterConfig.TorchServeContainerMemReqBytes < 0 {
		return nil, fmt.Errorf("%s environment variable must be set to a positive integer, found value %v",
			torchServeContainerMemReqBytes, adapterConfig.TorchServeContainerMemReqBytes)
	}
	if adapterConfig.ModelSizeMultiplier <= 0 {
		return nil, fmt.Errorf("%s environment variable must be greater than 0, found value %v",
			modelSizeMultiplier, adapterConfig.ModelSizeMultiplier)
	}
	return adapterConfig, nil
}
