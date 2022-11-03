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
package pvcprovider

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/go-logr/logr"

	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
	"github.com/kserve/modelmesh-runtime-adapter/pullman"
)

const (
	// config fields
	configPVCName      = "name"
	configPVCMountBase = "pvc_mount_base"

	// defaults
	defaultPVCMountBase = "/pvc_mounts"
)

type pvcProvider struct {
}

// pvcProvider implements StorageProvider
var _ pullman.StorageProvider = (*pvcProvider)(nil)

func (p pvcProvider) GetKey(config pullman.Config) string {
	// hash the values of all configurations that go into creating the client
	// no need to validate the config here
	pvcName, _ := pullman.GetString(config, configPVCName)
	pvcMountBase, _ := pullman.GetString(config, configPVCMountBase)

	return pullman.HashStrings(pvcName, pvcMountBase)
}

func (p pvcProvider) NewRepository(config pullman.Config, log logr.Logger) (pullman.RepositoryClient, error) {

	pvcName, _ := pullman.GetString(config, configPVCName)

	if pvcName == "" {
		return nil, errors.New("pvc name must be specified")
	}

	return &pvcRepositoryClient{
		log: log,
	}, nil
}

type pvcRepositoryClient struct {
	log logr.Logger
}

// pvcRepositoryClient implements RepositoryClient
var _ pullman.RepositoryClient = (*pvcRepositoryClient)(nil)

func (r *pvcRepositoryClient) Pull(ctx context.Context, pc pullman.PullCommand) error {
	targets := pc.Targets

	// Process per-command configuration
	pvcName, ok := pullman.GetString(pc.RepositoryConfig, configPVCName)
	if !ok {
		return fmt.Errorf("required configuration '%s' missing from command", configPVCName)
	}
	pvcMountBase, ok := pullman.GetString(pc.RepositoryConfig, configPVCMountBase)
	if !ok {
		// use the default value when not provided in repository config
		pvcMountBase = defaultPVCMountBase
	}

	pvcDir, joinErr := util.SecureJoin(pvcMountBase, pvcName)
	if joinErr != nil {
		return fmt.Errorf("unable to join paths '%s' and '%s': %v", pvcMountBase, pvcName, joinErr)
	}
	r.log.V(1).Info("The PVC directory is set", "pvcDir", pvcDir)

	// check the local PVC path /pvcMountBase/pvcName exists
	_, err := os.ReadDir(pvcDir)
	if err != nil {
		return fmt.Errorf("unable to read PVC directory '%s': %w", pvcDir, err)
	}

	for _, pt := range targets {
		fullModelPath, joinErr := util.SecureJoin(pvcDir, pt.RemotePath)
		if joinErr != nil {
			return fmt.Errorf("unable to join paths '%s' and '%s': %v", pvcDir, pt.RemotePath, joinErr)
		}
		r.log.V(1).Info("The model path is set", "fullModelPath", fullModelPath)

		// check the local model path /pvcMountBase/pvcName/modelPath exists
		if _, err = os.Stat(fullModelPath); os.IsNotExist(err) {
			return fmt.Errorf("unable to find model local path '%s'", fullModelPath)
		}
	}

	return nil
}

func init() {
	p := pvcProvider{}
	pullman.RegisterProvider("pvc", p)
}
