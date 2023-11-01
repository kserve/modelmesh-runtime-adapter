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
package puller

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-logr/logr"

	"github.com/kserve/modelmesh-runtime-adapter/internal/util"

	. "github.com/kserve/modelmesh-runtime-adapter/internal/envconfig"
)

// PullerConfiguration stores configuration variables for the puller server
type PullerConfiguration struct {
	RootModelDir            string // Root directory to store models
	StorageConfigurationDir string
}

// StorageConfiguration models the json credentials read from a storage secret
type StorageConfiguration struct {
	StorageType     string `json:"type"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	EndpointURL     string `json:"endpoint_url"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	// Deprecated: for backward compatibility DefaultBucket is still populated,
	// but new code should use Bucket instead.
	DefaultBucket string `json:"default_bucket"`
	Certificate   string `json:"certificate"`
}

// GetPullerConfigFromEnv creates a new PullerConfiguration populated from environment variables
func GetPullerConfigFromEnv(log logr.Logger) (*PullerConfiguration, error) {
	pullerConfig := new(PullerConfiguration)
	pullerConfig.RootModelDir = GetEnvString("ROOT_MODEL_DIR", "/models")
	pullerConfig.StorageConfigurationDir = GetEnvString("STORAGE_CONFIG_DIR", "/storage-config")

	return pullerConfig, nil
}

// GetStorageConfiguration returns configuration read from the mounted secret at the given key
func (config *PullerConfiguration) GetStorageConfiguration(storageKey string, log logr.Logger) (map[string]interface{}, error) {
	// TODO: cache the storage configs in memory and watch for changes
	// instead of reading from disk on each call

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

	bytes, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("Could not read storage configuration from %s: %v", configPath, err)
	}
	var storageConfig map[string]interface{}
	if err = json.Unmarshal(bytes, &storageConfig); err != nil {
		return nil, fmt.Errorf("Could not parse storage configuration json from %s: %v", configPath, err)
	}

	// copy fields where PullMan uses a different key for s3
	if storageConfig["type"] == "s3" {
		if default_bucket, exists := storageConfig["default_bucket"]; exists {
			// if both `bucket` and `default_bucket` exist, we're ignoring `default_bucket`
			if bucket, exists := storageConfig["bucket"]; exists {
				log.Info("Both bucket and default_bucket params were provided in S3 storage config, ignoring default_bucket", "bucket", bucket, "default_bucket", default_bucket)
			} else {
				storageConfig["bucket"] = default_bucket
			}
		}
	}

	return storageConfig, nil
}
