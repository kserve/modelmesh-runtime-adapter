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
package gcsprovider

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	"github.com/kserve/modelmesh-runtime-adapter/pullman"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func newGCSProviderWithMocks(t *testing.T) (gcsProvider, *MockgcsDownloaderFactory, logr.Logger) {
	mockCtrl := gomock.NewController(t)
	mdf := NewMockgcsDownloaderFactory(mockCtrl)
	g := gcsProvider{
		gcsDownloaderFactory: mdf,
	}
	log := zap.New()
	return g, mdf, log
}

func newGCSRepositoryClientWithMock(t *testing.T) (*gcsRepositoryClient, *MockgcsDownloader) {
	mockCtrl := gomock.NewController(t)
	md := NewMockgcsDownloader(mockCtrl)
	log := zap.New()
	gcsrc := gcsRepositoryClient{
		gcsclient: md,
		log:       log,
	}
	return &gcsrc, md
}

func Test_NewRepositoryWithCredentials(t *testing.T) {
	g, mdf, log := newGCSProviderWithMocks(t)
	c := pullman.NewRepositoryConfig("gcs", nil)

	privateKey := "private key"
	clientEmail := "client@email.com"
	c.Set("private_key", privateKey)
	c.Set("client_email", clientEmail)

	// Test that credentials are passed to newDownloader if specified.
	mdf.EXPECT().newDownloader(gomock.Any(), gomock.Eq(map[string]string{
		"private_key": privateKey, "client_email": clientEmail, "type": "service_account"})).Times(1)

	_, err := g.NewRepository(c, log)
	assert.NoError(t, err)

	tokenUri := "http://foo.bar"
	c.Set("token_uri", tokenUri)

	// Test that optional token_uri field is passed to newDownloader if specified.
	mdf.EXPECT().newDownloader(gomock.Any(), gomock.Eq(map[string]string{
		"private_key": privateKey, "client_email": clientEmail, "type": "service_account", "token_uri": tokenUri})).Times(1)

	_, err = g.NewRepository(c, log)
	assert.NoError(t, err)
}

func Test_NewRepositoryNoCredentials(t *testing.T) {
	g, mdf, log := newGCSProviderWithMocks(t)
	c := pullman.NewRepositoryConfig("gcs", nil)
	mdf.EXPECT().newDownloader(gomock.Any(), gomock.Nil()).Times(1)

	_, err := g.NewRepository(c, log)
	assert.NoError(t, err)
}

func Test_Download_SimpleDirectory(t *testing.T) {
	gcsRc, mdf := newGCSRepositoryClientWithMock(t)

	bucket := "bucket"
	c := pullman.NewRepositoryConfig("gcs", nil)
	c.Set("bucket", bucket)

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

	mdf.EXPECT().listObjects(context.Background(), gomock.Eq(bucket), gomock.Eq("path/to/modeldir")).
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
	mdf.EXPECT().downloadBatch(gomock.Any(), gomock.Eq(bucket), gomock.Eq(expectedTargets)).
		Return(nil).
		Times(1)

	err := gcsRc.Pull(context.Background(), inputPullCommand)
	assert.NoError(t, err)
}

func Test_Download_MultipleTargets(t *testing.T) {
	gcsRc, mdf := newGCSRepositoryClientWithMock(t)

	bucket := "bucket"
	c := pullman.NewRepositoryConfig("gcs", nil)
	c.Set("bucket", bucket)

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

	mdf.EXPECT().listObjects(context.Background(), gomock.Eq(bucket), gomock.Eq("dir")).
		Return([]string{"dir/file1", "dir/file2"}, nil).
		Times(1)
	mdf.EXPECT().listObjects(context.Background(), gomock.Eq(bucket), gomock.Eq("some_file")).
		Return([]string{"some_file"}, nil).
		Times(1)
	mdf.EXPECT().listObjects(context.Background(), gomock.Eq(bucket), gomock.Eq("another_dir")).
		Return([]string{"another_dir/another_file", "another_dir/subdir1/subdir2/nested_file"}, nil).
		Times(1)
	mdf.EXPECT().listObjects(context.Background(), gomock.Eq(bucket), gomock.Eq("another_file")).
		Return([]string{"another_file"}, nil).
		Times(1)
	mdf.EXPECT().listObjects(context.Background(), gomock.Eq(bucket), gomock.Eq("yet_another_file")).
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
	mdf.EXPECT().downloadBatch(gomock.Any(), gomock.Eq("bucket"), gomock.Eq(expectedTargets)).
		Return(nil).
		Times(1)

	err := gcsRc.Pull(context.Background(), inputPullCommand)
	assert.NoError(t, err)
}

func Test_GetKey(t *testing.T) {
	provider := gcsProvider{}

	createTestConfig := func() *pullman.RepositoryConfig {
		config := pullman.NewRepositoryConfig("gcs", nil)
		config.Set(configPrivateKey, "secret key")
		config.Set(configClientEmail, "user@email.com")
		return config
	}

	// should return the same result given the same config
	t.Run("shouldMatchForSameConfig", func(t *testing.T) {
		config1 := createTestConfig()
		config2 := createTestConfig()

		assert.Equal(t, provider.GetKey(config1), provider.GetKey(config2))
	})

	// changing the token_uri should change the key
	t.Run("shouldChangeForTokenUri", func(t *testing.T) {
		config1 := createTestConfig()
		config2 := createTestConfig()
		config2.Set(configTokenUri, "https://oauth2.googleapis.com/token2")

		assert.NotEqual(t, provider.GetKey(config1), provider.GetKey(config2))
	})

	// changing the bucket should NOT change the key
	t.Run("shouldNotChangeForBucket", func(t *testing.T) {
		config1 := createTestConfig()
		config2 := createTestConfig()
		config2.Set(configBucket, "another_bucket")

		assert.Equal(t, provider.GetKey(config1), provider.GetKey(config2))
	})
}
