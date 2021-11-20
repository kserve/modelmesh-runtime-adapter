// Copyright 2021 IBM Corporation
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
package puller

import (
	"fmt"
	"path/filepath"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	"github.com/kserve/modelmesh-runtime-adapter/model-serving-puller/generated/mocks"
	"github.com/kserve/modelmesh-runtime-adapter/pullman"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	TestDataDir       = "../testdata"
	StorageConfigDir  = filepath.Join(TestDataDir, "/storage-config")
	RootModelDir      = filepath.Join(TestDataDir, "/models")
	StorageConfigTest = StorageConfiguration{
		StorageType:     "s3",
		AccessKeyID:     "56421a5768b68451c86546d87654e467",
		SecretAccessKey: "abc643587654876546457985647863547865431786540521",
		EndpointURL:     "https://some-url.appdomain.cloud",
		Region:          "us-south",
		DefaultBucket:   "",
		Certificate:     "random-cert"}
)

func newPullerWithMock(t *testing.T) (*Puller, *mocks.MockPullerInterface) {
	log := zap.New()

	mockCtrl := gomock.NewController(t)
	mockPullManager := mocks.NewMockPullerInterface(mockCtrl)

	rootModelDir, err := filepath.Abs(RootModelDir)
	if err != nil {
		t.Fatalf("Failed to get absolute path of testdata dir %v", err)
	}

	pc := PullerConfiguration{
		RootModelDir:            rootModelDir,
		StorageConfigurationDir: StorageConfigDir,
	}
	pullerWithMock := NewPullerFromConfig(log, &pc)
	// inject mock pull manager
	pullerWithMock.PullManager = mockPullManager
	pullerWithMock.Log = log

	return pullerWithMock, mockPullManager
}

func Test_ProcessLoadModelRequest_Success_SingleFileModel(t *testing.T) {
	p, mockPuller := newPullerWithMock(t)

	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "tensorflow",
		ModelKey:  `{"storage_params":{"bucket":"bucket1"}, "storage_key": "myStorage"}`,
	}

	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "singlefile", "model.zip"),
		ModelType: "tensorflow",
		ModelKey:  `{"disk_size_bytes":60,"storage_key":"myStorage","storage_params":{"bucket":"bucket1"}}`,
	}

	expectedConfig := pullman.NewRepositoryConfig("s3")
	expectedConfig.Set("access_key_id", "")
	expectedConfig.Set("secret_access_key", "")
	expectedConfig.Set("endpoint_url", "")
	expectedConfig.Set("region", "")
	expectedConfig.Set("default_bucket", "default") // from myStorage
	expectedConfig.Set("bucket", "bucket1")         // from storage_params

	expectedPullCommand := pullman.PullCommand{
		RepositoryConfig: expectedConfig,
		Directory:        filepath.Join(p.PullerConfig.RootModelDir, "singlefile"),
		Targets: []pullman.Target{
			{
				RemotePath: "model.zip",
				LocalPath:  "model.zip",
			},
		},
	}

	mockPuller.EXPECT().Pull(gomock.Any(), gomock.Eq(expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(request)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
	assert.Nil(t, err)
}

func Test_ProcessLoadModelRequest_Success_MultiFileModel(t *testing.T) {
	p, mockPuller := newPullerWithMock(t)

	request := &mmesh.LoadModelRequest{
		ModelId:   "multifile",
		ModelPath: "path/to/model",
		ModelType: "tensorflow",
		ModelKey:  `{"storage_params":{"bucket":"bucket1"}, "storage_key": "myStorage"}`,
	}

	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "multifile",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "multifile", "model"),
		ModelType: "tensorflow",
		ModelKey:  `{"disk_size_bytes":60,"storage_key":"myStorage","storage_params":{"bucket":"bucket1"}}`,
	}

	expectedConfig := pullman.NewRepositoryConfig("s3")
	expectedConfig.Set("access_key_id", "")
	expectedConfig.Set("secret_access_key", "")
	expectedConfig.Set("endpoint_url", "")
	expectedConfig.Set("region", "")
	expectedConfig.Set("default_bucket", "default") // from myStorage
	expectedConfig.Set("bucket", "bucket1")         // from storage_params

	expectedPullCommand := pullman.PullCommand{
		RepositoryConfig: expectedConfig,
		Directory:        filepath.Join(p.PullerConfig.RootModelDir, "multifile"),
		Targets: []pullman.Target{
			{
				RemotePath: "path/to/model",
				LocalPath:  "model",
			},
		},
	}

	mockPuller.EXPECT().Pull(gomock.Any(), gomock.Eq(expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(request)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
	assert.Nil(t, err)
}

func Test_ProcessLoadModelRequest_SuccessWithSchema(t *testing.T) {
	p, mockPuller := newPullerWithMock(t)

	request := &mmesh.LoadModelRequest{
		ModelId:   "testmodel",
		ModelPath: "model.zip",
		ModelType: "tensorflow",
		ModelKey:  `{"storage_key": "myStorage", "storage_params":{"bucket":"bucket1"}, "schema_path": "my_schema"}`,
	}

	// expect updated schema_path in ModelKey
	expectedSchemaPath := filepath.Join(p.PullerConfig.RootModelDir, "testmodel", "my_schema")
	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "testmodel",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "testmodel", "model.zip"),
		ModelType: "tensorflow",
		ModelKey:  fmt.Sprintf(`{"disk_size_bytes":0,"schema_path":"%s","storage_key":"myStorage","storage_params":{"bucket":"bucket1"}}`, expectedSchemaPath),
	}

	expectedConfig := pullman.NewRepositoryConfig("s3")
	expectedConfig.Set("access_key_id", "")
	expectedConfig.Set("secret_access_key", "")
	expectedConfig.Set("endpoint_url", "")
	expectedConfig.Set("region", "")
	expectedConfig.Set("default_bucket", "default") // from myStorage
	expectedConfig.Set("bucket", "bucket1")

	expectedPullCommand := pullman.PullCommand{
		RepositoryConfig: expectedConfig,
		Directory:        filepath.Join(p.PullerConfig.RootModelDir, "testmodel"),
		Targets: []pullman.Target{
			{
				RemotePath: "model.zip",
				LocalPath:  "model.zip",
			},
			{
				RemotePath: "my_schema",
				LocalPath:  "my_schema",
			},
		},
	}

	mockPuller.EXPECT().Pull(gomock.Any(), gomock.Eq(expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(request)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
	assert.Nil(t, err)
}

func Test_ProcessLoadModelRequest_SuccessWithBucket(t *testing.T) {
	p, mockPuller := newPullerWithMock(t)

	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "tensorflow",
		ModelKey:  `{"bucket": "bucket1", "storage_key": "myStorage"}`,
	}

	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "singlefile", "model.zip"),
		ModelType: "tensorflow",
		ModelKey:  `{"bucket":"bucket1","disk_size_bytes":60,"storage_key":"myStorage"}`,
	}

	expectedConfig := pullman.NewRepositoryConfig("s3")
	expectedConfig.Set("access_key_id", "")
	expectedConfig.Set("secret_access_key", "")
	expectedConfig.Set("endpoint_url", "")
	expectedConfig.Set("region", "")
	expectedConfig.Set("default_bucket", "default") // from myStorage
	expectedConfig.Set("bucket", "bucket1")         // from storage_params

	expectedPullCommand := pullman.PullCommand{
		RepositoryConfig: expectedConfig,
		Directory:        filepath.Join(p.PullerConfig.RootModelDir, "singlefile"),
		Targets: []pullman.Target{
			{
				RemotePath: "model.zip",
				LocalPath:  "model.zip",
			},
		},
	}

	mockPuller.EXPECT().Pull(gomock.Any(), gomock.Eq(expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(request)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
	assert.Nil(t, err)
}

func Test_ProcessLoadModelRequest_SuccessNoBucket(t *testing.T) {
	p, mockPuller := newPullerWithMock(t)

	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "tensorflow",
		ModelKey:  `{"storage_params":{}, "storage_key": "myStorage"}`,
	}

	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "singlefile", "model.zip"),
		ModelType: "tensorflow",
		ModelKey:  `{"disk_size_bytes":60,"storage_key":"myStorage","storage_params":{}}`,
	}

	expectedConfig := pullman.NewRepositoryConfig("s3")
	expectedConfig.Set("access_key_id", "")
	expectedConfig.Set("secret_access_key", "")
	expectedConfig.Set("endpoint_url", "")
	expectedConfig.Set("region", "")
	expectedConfig.Set("default_bucket", "default") // from myStorage
	expectedConfig.Set("bucket", "default")         // from myStorage

	expectedPullCommand := pullman.PullCommand{
		RepositoryConfig: expectedConfig,
		Directory:        filepath.Join(p.PullerConfig.RootModelDir, "singlefile"),
		Targets: []pullman.Target{
			{
				RemotePath: "model.zip",
				LocalPath:  "model.zip",
			},
		},
	}

	mockPuller.EXPECT().Pull(gomock.Any(), gomock.Eq(expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(request)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
	assert.Nil(t, err)
}

func Test_ProcessLoadModelRequest_SuccessNoBucketNoStorageParams(t *testing.T) {
	p, mockPuller := newPullerWithMock(t)

	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "tensorflow",
		ModelKey:  `{"storage_key": "myStorage"}`,
	}

	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "singlefile", "model.zip"),
		ModelType: "tensorflow",
		ModelKey:  `{"disk_size_bytes":60,"storage_key":"myStorage"}`,
	}

	expectedConfig := pullman.NewRepositoryConfig("s3")
	expectedConfig.Set("access_key_id", "")
	expectedConfig.Set("secret_access_key", "")
	expectedConfig.Set("endpoint_url", "")
	expectedConfig.Set("region", "")
	expectedConfig.Set("default_bucket", "default") // from myStorage
	expectedConfig.Set("bucket", "default")         // from myStorage

	expectedPullCommand := pullman.PullCommand{
		RepositoryConfig: expectedConfig,
		Directory:        filepath.Join(p.PullerConfig.RootModelDir, "singlefile"),
		Targets: []pullman.Target{
			{
				RemotePath: "model.zip",
				LocalPath:  "model.zip",
			},
		},
	}

	mockPuller.EXPECT().Pull(gomock.Any(), gomock.Eq(expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(request)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
	assert.Nil(t, err)
}

func Test_ProcessLoadModelRequest_FailInvalidModelKey(t *testing.T) {
	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "tensorflow",
		ModelKey:  `{}{"storage_params":{"bucket":"bucket1"}, "storage_key": "myStorage"}`,
	}
	expectedError := fmt.Sprintf("Invalid modelKey in LoadModelRequest. ModelKey value '%s' is not valid JSON", request.ModelKey)

	p, _ := newPullerWithMock(t)

	returnRequest, err := p.ProcessLoadModelRequest(request)
	assert.Nil(t, returnRequest)
	assert.Contains(t, err.Error(), expectedError)
}

func Test_ProcessLoadModelRequest_FailInvalidSchemaPath(t *testing.T) {
	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "tensorflow",
		ModelKey:  `{"storage_params":{"bucket":"bucket1"}, "storage_key": "myStorage", "schema_path": 2}`,
	}
	expectedError := "Invalid schemaPath in LoadModelRequest, 'schema_path' attribute must have a string value. Found value 2"

	p, _ := newPullerWithMock(t)

	returnRequest, err := p.ProcessLoadModelRequest(request)
	assert.Nil(t, returnRequest)
	assert.EqualError(t, err, expectedError)
}

func Test_ProcessLoadModelRequest_FailMissingStorageKey(t *testing.T) {
	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "tensorflow",
		ModelKey:  `{"storage_params":{"bucket":"bucket1"}}`,
	}
	expectedError := "Predictor Storage field missing"

	p, _ := newPullerWithMock(t)

	returnRequest, err := p.ProcessLoadModelRequest(request)
	assert.Nil(t, returnRequest)
	assert.EqualError(t, err, expectedError)
}

func Test_getModelDiskSize(t *testing.T) {
	var diskSizeTests = []struct {
		modelPath    string
		expectedSize int64
	}{
		{"testModelSize/1/airbnb.model.lr.zip", 15259},
		{"testModelSize/1", 15259},
		{"testModelSize/2", 39375276},
	}

	for _, tt := range diskSizeTests {
		t.Run("", func(t *testing.T) {
			fullPath := filepath.Join(RootModelDir, tt.modelPath)
			diskSize, err := getModelDiskSize(fullPath)
			assert.NoError(t, err)
			assert.EqualValues(t, tt.expectedSize, diskSize)
		})
	}
}
