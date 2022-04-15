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
package azureprovider

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	gomock "github.com/golang/mock/gomock"
	"github.com/kserve/modelmesh-runtime-adapter/pullman"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	containerName = "foo"
	accountName   = "bar"
)

func newAzureProviderWithMocks(t *testing.T) (azureProvider, *MockazureDownloaderFactory, logr.Logger) {
	mockCtrl := gomock.NewController(t)
	mdf := NewMockazureDownloaderFactory(mockCtrl)
	g := azureProvider{
		azureDownloaderFactory: mdf,
	}
	log := zap.New()
	return g, mdf, log
}

func newAzureRepositoryClientWithMock(t *testing.T) (*azureRepositoryClient, *MockazureDownloader) {
	mockCtrl := gomock.NewController(t)
	md := NewMockazureDownloader(mockCtrl)
	log := zap.New()
	azurerc := azureRepositoryClient{
		azclient: md,
		log:      log,
	}
	return &azurerc, md
}

func Test_NewRepositoryWithServicePrincipalCredentials(t *testing.T) {
	g, mdf, log := newAzureProviderWithMocks(t)
	c := pullman.NewRepositoryConfig("azure", nil)

	tenantId := "abc"
	clientId := "123"
	clientSecret := "hunter2"
	c.Set(configContainer, containerName)
	c.Set(configAccountName, accountName)
	c.Set(configTenantId, tenantId)
	c.Set(configClientId, clientId)
	c.Set(configClientSecret, clientSecret)

	// Test that credentials are passed to newDownloader if specified.
	mdf.EXPECT().newDownloaderWithServicePrincipal(gomock.Any(), gomock.Any(), gomock.Eq(servicePrincipalCredentials{
		clientId: clientId, clientSecret: clientSecret, tenantId: tenantId})).Times(1)

	_, err := g.NewRepository(c, log)
	assert.NoError(t, err)
}

func Test_NewRepositoryWithConnectionStringCredentials(t *testing.T) {
	g, mdf, log := newAzureProviderWithMocks(t)
	c := pullman.NewRepositoryConfig("azure", nil)

	connectionString := "DefaultEndpointsProtocol=https;AccountName=foo;AccountKey=U0v4vD;EndpointSuffix=core.windows.net"
	c.Set(configContainer, containerName)
	c.Set(configAccountName, accountName)
	c.Set("connection_string", connectionString)

	mdf.EXPECT().newDownloaderWithConnectionString(gomock.Any(), containerName, connectionString).Times(1)

	_, err := g.NewRepository(c, log)
	assert.NoError(t, err)
}

func Test_NewRepositoryWithNoCredentials(t *testing.T) {
	g, mdf, log := newAzureProviderWithMocks(t)
	c := pullman.NewRepositoryConfig("azure", nil)

	c.Set(configContainer, containerName)
	c.Set(configAccountName, accountName)

	mdf.EXPECT().newDownloaderWithNoCredential(gomock.Any(), gomock.Any()).Times(1)

	_, err := g.NewRepository(c, log)
	assert.NoError(t, err)
}

func Test_Download_SimpleDirectory(t *testing.T) {
	azureRc, mdf := newAzureRepositoryClientWithMock(t)
	c := pullman.NewRepositoryConfig("azure", nil)
	c.Set(configContainer, containerName)

	downloadDir := filepath.Join("test", "output")
	inputPullCommand := pullman.PullCommand{
		RepositoryConfig: c,
		Directory:        downloadDir,
		Targets: []pullman.Target{
			{
				RemotePath: "path/to/modeldir",
			},
		},
	}

	mdf.EXPECT().listObjects(context.Background(), gomock.Eq("path/to/modeldir")).
		Return([]string{"path/to/modeldir/file.ext", "path/to/modeldir/subdir/another_file"}, nil).
		Times(1)

	expectedTargets := []pullman.Target{
		{
			RemotePath: "path/to/modeldir/file.ext",
			LocalPath:  filepath.Join(downloadDir, "file.ext"),
		},
		{
			RemotePath: "path/to/modeldir/subdir/another_file",
			LocalPath:  filepath.Join(downloadDir, "subdir", "another_file"),
		},
	}
	mdf.EXPECT().downloadBatch(gomock.Any(), gomock.Eq(expectedTargets)).
		Return(nil).
		Times(1)

	err := azureRc.Pull(context.Background(), inputPullCommand)
	assert.NoError(t, err)
}

func Test_Download_MultipleTargets(t *testing.T) {
	azureRc, mdf := newAzureRepositoryClientWithMock(t)

	c := pullman.NewRepositoryConfig("azure", nil)
	c.Set(configContainer, containerName)

	downloadDir := filepath.Join("test", "output")
	inputPullCommand := pullman.PullCommand{
		RepositoryConfig: c,
		Directory:        downloadDir,
		Targets: []pullman.Target{
			{
				RemotePath: "dir",
			},
			{
				// test that single file can be renamed
				RemotePath: "some_file",
				LocalPath:  "some_name.json",
			},
			{
				// test that a directory can be "renamed"
				RemotePath: "another_dir",
				LocalPath:  "local_another_dir",
			},
			{
				// test that single file can be renamed into subdirectory
				RemotePath: "another_file",
				LocalPath:  "local_another_dir/some_other_name.ext",
			},
			{
				// test that single file can pulled into a target directory
				RemotePath: "yet_another_file",
				LocalPath:  "local_another_dir/",
			},
		},
	}

	mdf.EXPECT().listObjects(context.Background(), gomock.Eq("dir")).
		Return([]string{"dir/file1", "dir/file2"}, nil).
		Times(1)
	mdf.EXPECT().listObjects(context.Background(), gomock.Eq("some_file")).
		Return([]string{"some_file"}, nil).
		Times(1)
	mdf.EXPECT().listObjects(context.Background(), gomock.Eq("another_dir")).
		Return([]string{"another_dir/another_file", "another_dir/subdir1/subdir2/nested_file"}, nil).
		Times(1)
	mdf.EXPECT().listObjects(context.Background(), gomock.Eq("another_file")).
		Return([]string{"another_file"}, nil).
		Times(1)
	mdf.EXPECT().listObjects(context.Background(), gomock.Eq("yet_another_file")).
		Return([]string{"yet_another_file"}, nil).
		Times(1)

	expectedTargets := []pullman.Target{
		{
			RemotePath: "dir/file1",
			LocalPath:  filepath.Join(downloadDir, "file1"),
		},
		{
			RemotePath: "dir/file2",
			LocalPath:  filepath.Join(downloadDir, "file2"),
		},
		{
			RemotePath: "some_file",
			LocalPath:  filepath.Join(downloadDir, "some_name.json"),
		},
		{
			RemotePath: "another_dir/another_file",
			LocalPath:  filepath.Join(downloadDir, "local_another_dir", "another_file"),
		},
		{
			RemotePath: "another_dir/subdir1/subdir2/nested_file",
			LocalPath:  filepath.Join(downloadDir, "local_another_dir", "subdir1/subdir2/nested_file"),
		},
		{
			RemotePath: "another_file",
			LocalPath:  filepath.Join(downloadDir, "local_another_dir", "some_other_name.ext"),
		},
		{
			RemotePath: "yet_another_file",
			LocalPath:  filepath.Join(downloadDir, "local_another_dir", "yet_another_file"),
		},
	}
	mdf.EXPECT().downloadBatch(gomock.Any(), gomock.Eq(expectedTargets)).
		Return(nil).
		Times(1)

	err := azureRc.Pull(context.Background(), inputPullCommand)
	assert.NoError(t, err)
}

func Test_GetKey(t *testing.T) {
	provider := azureProvider{}

	createTestConfig := func() *pullman.RepositoryConfig {
		config := pullman.NewRepositoryConfig("azure", nil)
		config.Set(configContainer, containerName)
		config.Set(configAccountName, accountName)
		return config
	}

	// should return the same result given the same config
	t.Run("shouldMatchForSameConfig", func(t *testing.T) {
		config1 := createTestConfig()
		config2 := createTestConfig()

		assert.Equal(t, provider.GetKey(config1), provider.GetKey(config2))
	})

	// Adding a credential should change the key
	t.Run("shouldChangeForTokenUri", func(t *testing.T) {
		config1 := createTestConfig()
		config2 := createTestConfig()
		config2.Set(configConnectionString, "abcdefg")

		assert.NotEqual(t, provider.GetKey(config1), provider.GetKey(config2))
	})

	// Changing account name should change the key
	t.Run("shouldChangeForTokenUri", func(t *testing.T) {
		config1 := createTestConfig()
		config2 := createTestConfig()
		config2.Set(configAccountName, "baz")

		assert.NotEqual(t, provider.GetKey(config1), provider.GetKey(config2))
	})
}
