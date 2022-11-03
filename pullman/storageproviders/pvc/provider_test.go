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
	log := zap.New()
	pvcrc := pvcRepositoryClient{
		log: log,
	}
	return &pvcrc
}

func Test_NewRepository(t *testing.T) {
	p, log := newPVCProvider(t)
	c := pullman.NewRepositoryConfig("pvc", nil)
	c.Set("name", "pvcName")
	_, err := p.NewRepository(c, log)
	assert.NoError(t, err)
}

func Test_Verify_Directory(t *testing.T) {
	pvcMountBase, _ := os.Getwd()
	pvcName := "pvcName"
	modelDir := "modelDir"
	pvcRc := newPVCRepositoryClient(t)
	c := pullman.NewRepositoryConfig("pvc", nil)
	c.Set("name", pvcName)
	c.Set("pvc_mount_base", pvcMountBase)

	pvcModelDir := pvcMountBase + "/" + pvcName + "/" + modelDir

	inputPullCommand := pullman.PullCommand{
		RepositoryConfig: c,
		Directory:        pvcModelDir,
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

	err = os.RemoveAll(pvcModelDir)
	assert.NoError(t, err)
}

func Test_GetKey(t *testing.T) {
	provider := pvcProvider{}

	createTestConfig := func() *pullman.RepositoryConfig {
		config := pullman.NewRepositoryConfig("pvc", nil)
		config.Set(configPVCName, "pvcName1")
		return config
	}

	// should return the same result given the same config
	t.Run("shouldMatchForSameConfig", func(t *testing.T) {
		config1 := createTestConfig()
		config2 := createTestConfig()

		assert.Equal(t, provider.GetKey(config1), provider.GetKey(config2))
	})

	// changing the pvc name should change the key
	t.Run("shouldChangeForTokenUri", func(t *testing.T) {
		config1 := createTestConfig()
		config2 := createTestConfig()
		config2.Set(configPVCName, "pvcName2")

		assert.NotEqual(t, provider.GetKey(config1), provider.GetKey(config2))
	})
}
