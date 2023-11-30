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
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"

	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
	"github.com/kserve/modelmesh-runtime-adapter/pullman"
)

const (
	// config fields
	configPVCName = "name"

	// defaults
	defaultPVCMountBase = "/pvc_mounts"
)

type pvcProvider struct {
	pvcMountBase string
}

// pvcProvider implements StorageProvider
var _ pullman.StorageProvider = (*pvcProvider)(nil)

func (p pvcProvider) GetKey(config pullman.Config) string {
	// Since the same instance of the repository can be used for all PVCs, there is no need to distinguish them here.
	return ""
}

func (p pvcProvider) NewRepository(config pullman.Config, log logr.Logger) (pullman.RepositoryClient, error) {
	if p.pvcMountBase == "" {
		p.pvcMountBase = defaultPVCMountBase
	}

	fileInfo, err := os.Stat(p.pvcMountBase)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("the PVC mount base '%s' doesn't exist: %w", p.pvcMountBase, err)
		}
		return nil, fmt.Errorf("failed to access the PVC mount base '%s': %w", p.pvcMountBase, err)
	}

	if !fileInfo.IsDir() {
		return nil, fmt.Errorf("the PVC mount base '%s' is not a directory", p.pvcMountBase)
	}

	return &pvcRepositoryClient{
		pvcProvider: p,
		log:         log,
	}, nil
}

type pvcRepositoryClient struct {
	pvcProvider pvcProvider
	log         logr.Logger
}

// pvcRepositoryClient implements RepositoryClient
var _ pullman.RepositoryClient = (*pvcRepositoryClient)(nil)

func (r *pvcRepositoryClient) Pull(ctx context.Context, pc pullman.PullCommand) error {
	targets := pc.Targets
	destDir := pc.Directory

	// Process per-command configuration
	pvcName, ok := pullman.GetString(pc.RepositoryConfig, configPVCName)
	if !ok {
		return fmt.Errorf("required configuration '%s' missing from command", configPVCName)
	}

	// create destination directories
	if mkdirErr := os.MkdirAll(destDir, 0755); mkdirErr != nil {
		return fmt.Errorf("unable to create directories '%s': %w", destDir, mkdirErr)
	}

	pvcDir, joinErr := util.SecureJoin(r.pvcProvider.pvcMountBase, pvcName)
	if joinErr != nil {
		return fmt.Errorf("unable to join paths '%s' and '%s': %v", r.pvcProvider.pvcMountBase, pvcName, joinErr)
	}
	r.log.V(1).Info("The PVC directory is set", "pvcDir", pvcDir)

	for _, pt := range targets {
		fullModelPath, joinErr := util.SecureJoin(pvcDir, pt.RemotePath)
		if joinErr != nil {
			return fmt.Errorf("unable to join paths '%s' and '%s': %v", pvcDir, pt.RemotePath, joinErr)
		}
		r.log.V(1).Info("The model path is set", "fullModelPath", fullModelPath)

		// check the local model path /pvcMountBase/pvcName/modelPath exists
		if _, err := os.Stat(fullModelPath); err != nil {
			return fmt.Errorf("unable to access model local path '%s': %w", fullModelPath, err)
		}

		// create symlink
		linkPath, err := util.SecureJoin(destDir, filepath.Base(fullModelPath))
		if err != nil {
			return fmt.Errorf("unable to join paths '%s' and '%s': %v", destDir, filepath.Base(fullModelPath), err)
		}
		if err := os.Symlink(fullModelPath, linkPath); err != nil {
			return fmt.Errorf("unable to create symlink '%s': %w", linkPath, err)
		}
	}

	return nil
}

func init() {
	p := pvcProvider{}
	pullman.RegisterProvider("pvc", p)
}
