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
	"testing"
)

func Test_ConfigParsing(t *testing.T) {
	configMapJSON := `{
		"test-repo-name": {
			"type": "s3",
			"access_key_id": "ACCESS_KEY",
			"secret_access_key": "SECRET_ACCESS_KEY",
			"endpoint_url": "https://s3.us-south.cloud-object-storage.appdomain.cloud",
			"region": "us-south",
			"bucket": "wml-serving-models"
		}

	}`

	var configMap map[string]RepositoryConfig
	json.Unmarshal([]byte(configMapJSON), &configMap)

	c, exists := configMap["test-repo-name"]
	if !exists {
		t.Error("expected parsed config map to have config")
	}

	if c.GetType() != "s3" {
		t.Error("expected config type to be 's3'")
	}
}
