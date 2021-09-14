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
package puller

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/go-logr/logr"

	. "github.com/kserve/modelmesh-runtime-adapter/internal/envconfig"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
)

// PullerConfiguration stores configuration variables for the puller server
type PullerConfiguration struct {
	RootModelDir            string // Root directory to store models
	StorageConfigurationDir string
	S3DownloadConcurrency   int
	CacheCleanPeriod        time.Duration
}

// StorageConfiguration models the json credentials read from a storage secret
type StorageConfiguration struct {
	StorageType     string `json:"type"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	EndpointURL     string `json:"endpoint_url"`
	Region          string `json:"region"`
	DefaultBucket   string `json:"default_bucket"`
	Certificate     string `json:"certificate"`
}

// GetPullerConfigFromEnv creates a new PullerConfiguration populated from environment variables
func GetPullerConfigFromEnv(log logr.Logger) (*PullerConfiguration, error) {
	pullerConfig := new(PullerConfiguration)
	pullerConfig.RootModelDir = GetEnvString("ROOT_MODEL_DIR", "/models")
	pullerConfig.StorageConfigurationDir = GetEnvString("STORAGE_CONFIG_DIR", "/storage-config")
	pullerConfig.S3DownloadConcurrency = GetEnvInt("S3_DOWNLOAD_CONCURRENCY", 10, log)

	v := GetEnvString("PULLER_CACHE_CLEAN_PERIOD", "24h")
	d, err := time.ParseDuration(v)
	if err != nil {
		return pullerConfig, fmt.Errorf("Failed to parse duration from environment variable with content [%s]: %w", v, err)
	}
	pullerConfig.CacheCleanPeriod = d

	return pullerConfig, nil
}

// GetStorageConfiguration returns a StorageConfiguration read from the mounted secret at the give key
func (config *PullerConfiguration) GetStorageConfiguration(storageKey string, log logr.Logger) (*StorageConfiguration, error) {
	configPath, err := util.SecureJoin(config.StorageConfigurationDir, storageKey)
	if err != nil {
		log.Error(err, "Error joining paths", "directory", config.StorageConfigurationDir, "key", storageKey)
		return nil, err
	}

	log.V(1).Info("Reading storage credentials")

	if _, err = os.Stat(configPath); os.IsNotExist(err) {
		// TODO consider a retry period in case the secret isn't updated yet
		return nil, fmt.Errorf("Storage secretKey not found: %s", storageKey)
	}

	bytes, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("Could not read storage configuration from %s: %v", configPath, err)
	}
	var storageConfig StorageConfiguration
	if err = json.Unmarshal(bytes, &storageConfig); err != nil {
		return nil, fmt.Errorf("Could not parse storage configuration json from %s: %v", configPath, err)
	}

	return &storageConfig, nil
}
