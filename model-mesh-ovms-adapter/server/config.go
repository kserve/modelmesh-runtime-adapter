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

import (
	"fmt"
	"time"

	"github.com/kserve/modelmesh-runtime-adapter/internal/util"

	"github.com/go-logr/logr"

	. "github.com/kserve/modelmesh-runtime-adapter/internal/envconfig"
)

const (
	adapterPort                     string = "ADAPTER_PORT"
	defaultAdapterPort                     = 8085
	runtimePort                     string = "RUNTIME_PORT"
	defaultRuntimePort                     = 8001
	ovmsContainerMemReqBytes        string = "CONTAINER_MEM_REQ_BYTES"
	defaultOvmsContainerMemReqBytes        = -1
	ovmsMemBufferBytes              string = "MEM_BUFFER_BYTES"
	defaultOvmsMemBufferBytes              = 256 * 1024 * 1024 // 256MB
	maxLoadingConcurrency           string = "LOADING_CONCURRENCY"
	defaultMaxLoadingConcurrency           = 1
	maxLoadingTimeoutMs             string = "LOADTIME_TIMEOUT"
	defaultMaxLoadingTimeoutMs             = 30000
	defaultModelSize                string = "DEFAULT_MODELSIZE"
	defaultModelSizeInBytes                = 1000000
	modelSizeMultiplier             string = "MODELSIZE_MULTIPLIER"
	defaultModelSizeMultiplier             = 1.25
	runtimeVersion                  string = "RUNTIME_VERSION"
	defaultRuntimeVersion                  = "v1"
	limitPerModelConcurrency        string = "LIMIT_PER_MODEL_CONCURRENCY"
	defaultLimitPerModelConcurrency        = 0 // 0 means don't limit request concurrency
	rootModelDir                    string = "ROOT_MODEL_DIR"
	defaultRootModelDir                    = "/models"
	useEmbeddedPuller               string = "USE_EMBEDDED_PULLER"
	defaultUseEmbeddedPuller               = false

	// OVMS adapter specific
	modelConfigFile         string = "MODEL_CONFIG_FILE"
	defaultModelConfigFile         = "/models/model_config_list.json"
	batchWaitTimeMin        string = "BATCH_WAIT_TIME_MIN"
	defaultBatchWaitTimeMin        = 100 * time.Millisecond
	batchWaitTimeMax        string = "BATCH_WAIT_TIME_MAX"
	defaultBatchWaitTimeMax        = 3 * time.Second
	reloadTimeout           string = "OVMS_RELOAD_TIMEOUT"
	defaultReloadTimeout           = 30 * time.Second
)

func GetAdapterConfigurationFromEnv(log logr.Logger) (*AdapterConfiguration, error) {
	adapterConfig := new(AdapterConfiguration)
	adapterConfig.Port = GetEnvInt(adapterPort, defaultAdapterPort, log)
	adapterConfig.OvmsPort = GetEnvInt(runtimePort, defaultRuntimePort, log)
	adapterConfig.OvmsContainerMemReqBytes = GetEnvInt(ovmsContainerMemReqBytes, defaultOvmsContainerMemReqBytes, log)
	adapterConfig.OvmsMemBufferBytes = GetEnvInt(ovmsMemBufferBytes, defaultOvmsMemBufferBytes, log)
	adapterConfig.CapacityInBytes = adapterConfig.OvmsContainerMemReqBytes - adapterConfig.OvmsMemBufferBytes
	adapterConfig.MaxLoadingConcurrency = GetEnvInt(maxLoadingConcurrency, defaultMaxLoadingConcurrency, log)
	adapterConfig.ModelLoadingTimeoutMS = GetEnvInt(maxLoadingTimeoutMs, defaultMaxLoadingTimeoutMs, log)
	adapterConfig.DefaultModelSizeInBytes = GetEnvInt(defaultModelSize, defaultModelSizeInBytes, log)
	adapterConfig.ModelSizeMultiplier = GetEnvFloat(modelSizeMultiplier, defaultModelSizeMultiplier, log)
	adapterConfig.RuntimeVersion = GetEnvString(runtimeVersion, defaultRuntimeVersion)
	adapterConfig.LimitModelConcurrency = GetEnvInt(limitPerModelConcurrency, defaultLimitPerModelConcurrency, log)
	adapterConfig.UseEmbeddedPuller = GetEnvBool(useEmbeddedPuller, defaultUseEmbeddedPuller, log)

	var err error
	adapterConfig.RootModelDir, err = util.SecureJoin(GetEnvString(rootModelDir, defaultRootModelDir), ovmsModelSubdir)
	if err != nil {
		return nil, fmt.Errorf("Could not construct model store path: %w", err)
	}

	// OVMS adapter specific
	adapterConfig.ModelConfigFile = GetEnvString(modelConfigFile, defaultModelConfigFile)
	adapterConfig.BatchWaitTimeMin = GetEnvDuration(batchWaitTimeMin, defaultBatchWaitTimeMin, log)
	adapterConfig.BatchWaitTimeMax = GetEnvDuration(batchWaitTimeMax, defaultBatchWaitTimeMax, log)
	adapterConfig.ReloadTimeout = GetEnvDuration(reloadTimeout, defaultReloadTimeout, log)

	if adapterConfig.OvmsContainerMemReqBytes < 0 {
		return nil, fmt.Errorf("%s environment variable must be set to a positive integer, found value %v", ovmsContainerMemReqBytes, adapterConfig.OvmsContainerMemReqBytes)
	}
	if adapterConfig.ModelSizeMultiplier <= 0 {
		return nil, fmt.Errorf("%s environment variable must be greater than 0, found value %v", modelSizeMultiplier, adapterConfig.ModelSizeMultiplier)
	}
	return adapterConfig, nil
}
