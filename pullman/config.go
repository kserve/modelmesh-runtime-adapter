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
package pullman

import (
	"encoding/json"
	"fmt"
)

const (
	storageTypeKey = "type"
)

// Config represents simple key/value configuration with a type/class
type Config interface {
	// GetType returns the type of the config
	GetType() string

	// Get returns a key's value and a bool if it was specified
	Get(name string) (interface{}, bool)
}

// helper functions to have consistent behavior when working with Configs
func GetString(c Config, key string) (string, bool) {
	val, exists := c.Get(key)
	if !exists {
		return "", false
	}

	// if it is not a string, treat it as if it does not exist
	s, ok := val.(string)
	return s, ok
}

// Generic config abstraction used by PullMan
type RepositoryConfig struct {
	config      map[string]interface{}
	storageType string
}

// RepositoryConfig implements Config
var _ Config = (*RepositoryConfig)(nil)

func NewRepositoryConfig(storageType string, config map[string]interface{}) *RepositoryConfig {
	if config == nil {
		config = make(map[string]interface{})
	}
	// storageType takes priority
	config[storageTypeKey] = storageType
	return &RepositoryConfig{
		config:      config,
		storageType: storageType,
	}
}

func (rc *RepositoryConfig) GetType() string {
	return rc.storageType
}

func (rc *RepositoryConfig) Get(key string) (interface{}, bool) {
	val, exists := rc.config[key]
	return val, exists
}

func (rc *RepositoryConfig) GetString(key string) (string, bool) {
	val, exists := rc.Get(key)
	if !exists {
		return "", false
	}

	// if it is not a string, treat it as if it does not exist
	s, ok := val.(string)
	return s, ok
}

func (rc *RepositoryConfig) Set(key string, val interface{}) {
	// check if we are updating the stored type
	if key == storageTypeKey {
		// only update if value is a string
		if st, ok := val.(string); ok {
			rc.storageType = st
		}
	}
	rc.config[key] = val
}

func (rc *RepositoryConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(rc.config)
}

func (rc *RepositoryConfig) UnmarshalJSON(bs []byte) error {
	var storageMap map[string]interface{}
	if err := json.Unmarshal(bs, &storageMap); err != nil {
		return fmt.Errorf("error parsing config JSON: %w", err)
	}
	// ensure the storage type key exists in the config
	if _, ok := storageMap[storageTypeKey]; !ok {
		return fmt.Errorf("storage config missing `%s` field", storageTypeKey)
	}
	// and that its value is a string
	st, ok := storageMap[storageTypeKey].(string)
	if !ok {
		return fmt.Errorf("storage config field `%s` must have a string value", storageTypeKey)
	}
	rc.storageType = st
	rc.config = storageMap
	return nil
}
