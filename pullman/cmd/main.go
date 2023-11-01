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
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/go-logr/zapr"
	"go.uber.org/zap"

	"github.com/kserve/modelmesh-runtime-adapter/pullman"
	_ "github.com/kserve/modelmesh-runtime-adapter/pullman/storageproviders/http"
	_ "github.com/kserve/modelmesh-runtime-adapter/pullman/storageproviders/s3"
)

// Basic CLI to pull files with PullMan
//
// Example Config File:
// {
//   "example": {
//     "type": "s3",
//       "access_key": "ACCESS_KEY",
//       "secret_access_key": "SECRET_ACCESS_KEY",
//       "endpoint_url": "https://s3.us-south.cloud-object-storage.appdomain.cloud",
//       "region": "us-south",
//       "bucket": "wml-serving-models"
//   }
// }
//
// Example Usage:
// go run cmd/main.go -config storage-config.json -repo example "wml-serving-models/model" "wml-serving-models/model_schema.json"

func main() {
	var configFilename string
	var repositoryName string
	var outputDirectory string
	var paths []string
	flag.StringVar(&configFilename, "config", "storage-config.json", "Path to the storage configuration file")
	flag.StringVar(&repositoryName, "repo", "", "Name of the repository in the config file to pull from")
	flag.StringVar(&outputDirectory, "outdir", "./puller_output", "Name of the directory files will be pulled to")
	flag.Parse()

	paths = flag.Args()

	// set-up the logger
	zaplog, err := zap.NewDevelopment()
	if err != nil {
		panic("Error creating logger...")
	}
	manager := pullman.NewPullManager(zapr.NewLogger(zaplog))

	// create repository from the config file
	configMapJSON, err := os.ReadFile(configFilename)
	if err != nil {
		fmt.Printf("Error reading config file [%s]: %v\n", configFilename, err)
		return
	}

	var configMap map[string]pullman.RepositoryConfig
	if err = json.Unmarshal(configMapJSON, &configMap); err != nil {
		fmt.Printf("Error unmarshaling config: %v\n", err)
		return
	}

	rc, ok := configMap[repositoryName]
	if !ok {
		fmt.Printf("Repository with name [%s] not found in config\n", repositoryName)
		return
	}

	// build pull command
	pts := make([]pullman.Target, len(paths))
	for i, p := range paths {
		pts[i] = pullman.Target{
			RemotePath: p,
		}
	}
	pc := pullman.PullCommand{
		RepositoryConfig: &rc,
		Directory:        outputDirectory,
		Targets:          pts,
	}

	pullErr := manager.Pull(context.Background(), pc)
	if pullErr != nil {
		fmt.Printf("Failed to pull files: %v\n", pullErr)
	}
}
