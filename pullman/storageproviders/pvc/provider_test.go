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
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/kserve/modelmesh-runtime-adapter/pullman"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func newPVCProvider(t *testing.T) (pvcProvider, logr.Logger) {
	p := pvcProvider{}
	log := zap.New()
	return p, log
}

func newPVCRepositoryClient(t *testing.T) *pvcRepositoryClient {
	pvcMountBase, _ := os.Getwd()
	p, log := newPVCProvider(t)
	p.pvcMountBase = pvcMountBase
	pvcrc := pvcRepositoryClient{
		pvcProvider: p,
		log:         log,
	}
	return &pvcrc
}

func Test_NewRepository(t *testing.T) {
	pvcMountBase, _ := os.Getwd()
	p, log := newPVCProvider(t)
	p.pvcMountBase = pvcMountBase
	c := pullman.NewRepositoryConfig("pvc", nil)
	c.Set("name", "pvcName")
	_, err := p.NewRepository(c, log)
	assert.NoError(t, err)
}

func Test_Verify_Directory(t *testing.T) {
	pvcName := "pvcName"
	modelDir := "modelDir"
	modelID := "testId"
	pvcRc := newPVCRepositoryClient(t)
	c := pullman.NewRepositoryConfig("pvc", nil)
	c.Set("name", pvcName)

	pvcNameDir := pvcRc.pvcProvider.pvcMountBase + "/" + pvcName
	pvcModelDir := pvcNameDir + "/" + modelDir
	serveModelDir := pvcRc.pvcProvider.pvcMountBase + "/" + modelID

	inputPullCommand := pullman.PullCommand{
		RepositoryConfig: c,
		Directory:        serveModelDir,
		Targets: []pullman.Target{
			{
				RemotePath: modelDir,
			},
		},
	}

	// should return error because the directory doesn't exist
	err := pvcRc.Pull(context.Background(), inputPullCommand)
	assert.Error(t, err)

	err = os.MkdirAll(pvcModelDir, os.ModePerm)
	assert.NoError(t, err)

	// should not return error because the directory exists
	err = pvcRc.Pull(context.Background(), inputPullCommand)
	assert.NoError(t, err)

	err = os.RemoveAll(pvcNameDir)
	assert.NoError(t, err)

	err = os.RemoveAll(serveModelDir)
	assert.NoError(t, err)
}

func Test_GetKey(t *testing.T) {
	provider := pvcProvider{}

	createTestConfig := func(pvcName string) *pullman.RepositoryConfig {
		config := pullman.NewRepositoryConfig("pvc", nil)
		config.Set(configPVCName, pvcName)
		return config
	}

	// different pvc names should have the same key
	t.Run("shouldChangeForTokenUri", func(t *testing.T) {
		config1 := createTestConfig("pvcName1")
		config2 := createTestConfig("pvcName2")
		assert.Equal(t, provider.GetKey(config1), provider.GetKey(config2))
	})
}
