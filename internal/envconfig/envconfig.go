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
package envconfig

import (
	"os"
	"strconv"
	"time"

	"github.com/go-logr/logr"
)

// Returns the string value of environment variable "key" or the default value
// if "key" is not set. Note if the environment variable is set to an empty
// string, this will return an empty string, not defaultValue.
func GetEnvString(key string, defaultValue string) string {
	if val, found := os.LookupEnv(key); found {
		return val
	}
	return defaultValue
}

// Returns the integer of value environment variable "key" or the default value
// if "key" is not set. Note if the environment variable is set to a non
// integer, including an empty string, this will fail and exit.
func GetEnvInt(key string, defaultValue int, log logr.Logger) int {
	if strVal, found := os.LookupEnv(key); found {
		val, err := strconv.Atoi(strVal)
		if err != nil {
			log.Error(err, "Environment variable must be an int", "env_var", key, "value", strVal)
			os.Exit(1)
		}
		return val
	}
	return defaultValue
}

func GetEnvFloat(key string, defaultValue float64, log logr.Logger) float64 {
	if strVal, found := os.LookupEnv(key); found {
		val, err := strconv.ParseFloat(strVal, 64)
		if err != nil {
			log.Error(err, "Environment variable must be a number", "environment_variable", key, "value", strVal)
			os.Exit(1)
		}
		return val
	}
	return defaultValue
}

// Returns the bool value of environment variable "key" or the default value
// if "key" is not set. Note if the environment variable is set to a non
// boolean, including an empty string, this will fail and exit.
func GetEnvBool(key string, defaultValue bool, log logr.Logger) bool {
	if strVal, found := os.LookupEnv(key); found {
		val, err := strconv.ParseBool(strVal)
		if err != nil {
			log.Error(err, "Environment variable must be boolean", "env_var", key, "value", strVal)
			os.Exit(1)
		}
		return val
	}
	return defaultValue
}

// Returns the duration value of environment variable "key" or a default value
// Note if the environment variable cannot be parsed as a duration, including an
// empty string, this will fail and exit.
func GetEnvDuration(key string, defaultValue time.Duration, log logr.Logger) time.Duration {
	if strVal, found := os.LookupEnv(key); found {
		val, err := time.ParseDuration(strVal)
		if err != nil {
			log.Error(err, "Environment variable must be a duration", "env_var", key, "value", strVal)
			os.Exit(1)
		}
		return val
	}
	return defaultValue
}
