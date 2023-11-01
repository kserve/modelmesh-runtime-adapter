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
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// tests the mockStorageProvider registered with type "mock"
var mockProviderType = "mock"

func newPullManagerWithMock(ctrl *gomock.Controller) (*PullManager, *MockStorageProvider) {
	log := zap.New()

	msp := NewMockStorageProvider()
	pm := NewPullManager(log)
	pm.storageProviders = map[string]StorageProvider{
		mockProviderType: msp,
	}

	return pm, msp
}

// Tests of the Cacheing

func Test_UseTwoClients_WithCompatibleConfigs(t *testing.T) {
	ctrl := gomock.NewController(t)

	pm, msp := newPullManagerWithMock(ctrl)
	mrc := NewMockRepositoryClient(ctrl)

	ckey := ""
	msp.RegisterMockClient(ckey, mrc)

	mrcConfig := NewRepositoryConfig(mockProviderType, nil)
	mrcConfig.Set(mockConfigKey, ckey)

	{
		pc := PullCommand{
			RepositoryConfig: mrcConfig,
			Targets:          []Target{},
		}
		ctx := context.Background()
		mrc.EXPECT().Pull(ctx, pc).Return(nil).Times(1)
		err := pm.Pull(ctx, pc)
		assert.NoError(t, err)
		// should have one cached client
		assert.Equal(t, 1, len(pm.clientCache.cache))
	}

	// modify the config, but keep the same key
	mrcConfig.Set("random_key", "some_value")

	{
		pc := PullCommand{
			RepositoryConfig: mrcConfig,
			Targets:          []Target{},
		}
		ctx := context.Background()
		mrc.EXPECT().Pull(ctx, pc).Return(nil).Times(1)
		err := pm.Pull(ctx, pc)
		assert.NoError(t, err)
		// should still have one cached client (reused previous one)
		assert.Equal(t, 1, len(pm.clientCache.cache))
	}
}

func Test_UseTwoClients_WithIncompatibleConfigs(t *testing.T) {
	ctrl := gomock.NewController(t)

	pm, msp := newPullManagerWithMock(ctrl)

	mrc1 := NewMockRepositoryClient(ctrl)
	key1 := "first key"
	msp.RegisterMockClient(key1, mrc1)
	mrcConfig1 := NewRepositoryConfig(mockProviderType, nil)
	mrcConfig1.Set(mockConfigKey, key1)

	mrc2 := NewMockRepositoryClient(ctrl)
	key2 := "second key"
	msp.RegisterMockClient(key2, mrc2)
	mrcConfig2 := NewRepositoryConfig(mockProviderType, nil)
	mrcConfig2.Set(mockConfigKey, key2)

	{
		pc := PullCommand{
			RepositoryConfig: mrcConfig1,
			Targets:          []Target{},
		}
		ctx := context.Background()
		mrc1.EXPECT().Pull(ctx, pc).Return(nil).Times(1)
		err := pm.Pull(ctx, pc)
		assert.NoError(t, err)
		// should have one cached client
		assert.Equal(t, 1, len(pm.clientCache.cache))
	}

	{
		pc := PullCommand{
			RepositoryConfig: mrcConfig2,
			Targets:          []Target{},
		}
		ctx := context.Background()
		mrc2.EXPECT().Pull(ctx, pc).Return(nil).Times(1)
		err := pm.Pull(ctx, pc)
		assert.NoError(t, err)
		// now should have two clients cached
		assert.Equal(t, 2, len(pm.clientCache.cache))
	}
}

func Test_Errors_InvalidConfig(t *testing.T) {
	ctrl := gomock.NewController(t)

	pm, msp := newPullManagerWithMock(ctrl)

	mrc := NewMockRepositoryClient(ctrl)
	key := ""
	msp.RegisterMockClient(key, mrc)
	mrcConfig := NewRepositoryConfig(mockProviderType, nil)
	errString := "mock error for an invalid configuration"
	mrcConfig.Set(mockConfigError, errors.New(errString))

	pc := PullCommand{
		RepositoryConfig: mrcConfig,
		Targets:          []Target{},
	}
	err := pm.Pull(context.Background(), pc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errString)
	// should have no cached client
	assert.Equal(t, 0, len(pm.clientCache.cache))
}
