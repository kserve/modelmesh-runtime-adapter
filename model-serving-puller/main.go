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
	"strings"

	"github.com/go-logr/logr"
	_ "github.com/joho/godotenv/autoload"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kserve/modelmesh-runtime-adapter/internal/envconfig"
	"github.com/kserve/modelmesh-runtime-adapter/model-serving-puller/server"
)

func main() {
	var log logr.Logger
	if strings.ToLower(envconfig.GetEnvString("LOG_LEVEL", "debug")) == "debug" {
		log = zap.New(zap.UseDevMode(true))
	} else {
		log = zap.New()
	}

	pServer := server.NewPullerServer(log)

	err := pServer.StartServer()

	if err == nil {
		log.Info("*** Server terminated")
	} else {
		log.Info("*** Server terminated with error: ", err)
	}
}
