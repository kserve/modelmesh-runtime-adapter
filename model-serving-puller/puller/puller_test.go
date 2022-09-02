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
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
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

// custom GoMock matcher for a PullCommand
type pullCommandMatcher struct {
	expected *pullman.PullCommand
}

func (pcm pullCommandMatcher) Matches(pci interface{}) bool {
	pc := pci.(pullman.PullCommand)
	if pc.Directory != pcm.expected.Directory {
		return false
	}

	if !gomock.Eq(pcm.expected.Targets).Matches(pc.Targets) {
		return false
	}

	expectedConfigJSON, err := json.Marshal(pcm.expected.RepositoryConfig)
	if err != nil {
		return false
	}

	gotConfigJSON, err := json.Marshal(pc.RepositoryConfig)
	if err != nil {
		return false
	}

	if string(expectedConfigJSON) == string(gotConfigJSON) {
		return true
	}

	return false
}

func (pcm pullCommandMatcher) String() string {
	return describePullCommand(pcm.expected)
}

func describePullCommand(pc *pullman.PullCommand) string {
	return fmt.Sprintf("PullCommand\n\tDirectory: %s\n\tTargets: %v\n\tConfig: %v", pc.Directory, pc.Targets, pc.RepositoryConfig)
}

func eqPullCommand(pc *pullman.PullCommand) gomock.Matcher {
	return gomock.GotFormatterAdapter(
		gomock.GotFormatterFunc(func(pci interface{}) string {
			pc, _ := pci.(pullman.PullCommand)
			return describePullCommand(&pc)
		}),
		pullCommandMatcher{expected: pc},
	)
}

// helper to create a RepositoryConfig from the testdata storage config dir
func readStorageConfig(key string) (*pullman.RepositoryConfig, error) {

	j, err := ioutil.ReadFile(filepath.Join(StorageConfigDir, key))
	if err != nil {
		return nil, err
	}

	var rcMap map[string]interface{}
	if err = json.Unmarshal(j, &rcMap); err != nil {
		return nil, err
	}

	var storageType string
	if t, ok := rcMap["type"].(string); !ok {
		return nil, fmt.Errorf(`Storage config must have a "type" field with string value`)
	} else {
		storageType = t
	}

	return pullman.NewRepositoryConfig(storageType, rcMap), nil
}

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
		ModelType: "rt:triton",
		ModelKey:  `{"storage_params":{"bucket":"bucket1"}, "storage_key": "myStorage", "model_type": {"name": "tensorflow"}}`,
	}

	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "singlefile", "model.zip"),
		ModelType: "rt:triton",
		ModelKey:  `{"model_type":{"name":"tensorflow"},"disk_size_bytes":60}`,
	}

	expectedConfig, err := readStorageConfig("myStorage")
	assert.NoError(t, err)
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

	mockPuller.EXPECT().Pull(gomock.Any(), eqPullCommand(&expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(context.Background(), request)
	assert.Nil(t, err)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
}

func Test_ProcessLoadModelRequest_Success_MultiFileModel(t *testing.T) {
	p, mockPuller := newPullerWithMock(t)

	request := &mmesh.LoadModelRequest{
		ModelId:   "multifile",
		ModelPath: "path/to/model",
		ModelType: "mt:tensorflow",
		ModelKey:  `{"storage_params":{"bucket":"bucket1"}, "storage_key": "myStorage", "model_type": {"name": "tensorflow"}}`,
	}

	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "multifile",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "multifile", "model"),
		ModelType: "mt:tensorflow",
		ModelKey:  `{"model_type":{"name":"tensorflow"},"disk_size_bytes":60}`,
	}

	expectedConfig, err := readStorageConfig("myStorage")
	assert.NoError(t, err)
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

	mockPuller.EXPECT().Pull(gomock.Any(), eqPullCommand(&expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(context.Background(), request)
	assert.Nil(t, err)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
}

func Test_ProcessLoadModelRequest_SuccessWithSchema(t *testing.T) {
	p, mockPuller := newPullerWithMock(t)

	request := &mmesh.LoadModelRequest{
		ModelId:   "testmodel",
		ModelPath: "model.zip",
		ModelType: "rt:triton",
		ModelKey:  `{"storage_key": "myStorage", "storage_params":{"bucket":"bucket1"}, "schema_path": "my_schema", "model_type": {"name": "tensorflow"}}`,
	}

	// expect updated schema_path in ModelKey
	expectedSchemaPath := filepath.Join(p.PullerConfig.RootModelDir, "testmodel", "my_schema")
	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "testmodel",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "testmodel", "model.zip"),
		ModelType: "rt:triton",
		ModelKey:  fmt.Sprintf(`{"model_type":{"name":"tensorflow"},"disk_size_bytes":0,"schema_path":"%s"}`, expectedSchemaPath),
	}

	expectedConfig, err := readStorageConfig("myStorage")
	assert.NoError(t, err)
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

	mockPuller.EXPECT().Pull(gomock.Any(), eqPullCommand(&expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(context.Background(), request)
	assert.Nil(t, err)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
}

func Test_ProcessLoadModelRequest_SuccessWithBucket(t *testing.T) {
	p, mockPuller := newPullerWithMock(t)

	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "mt:tensorflow",
		ModelKey:  `{"bucket": "bucket1", "storage_key": "myStorage", "model_type": {"name": "tensorflow"}}`,
	}

	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "singlefile", "model.zip"),
		ModelType: "mt:tensorflow",
		ModelKey:  `{"model_type":{"name":"tensorflow"},"disk_size_bytes":60}`,
	}

	expectedConfig, err := readStorageConfig("myStorage")
	assert.NoError(t, err)
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

	mockPuller.EXPECT().Pull(gomock.Any(), eqPullCommand(&expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(context.Background(), request)
	assert.Nil(t, err)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
}

func Test_ProcessLoadModelRequest_SuccessNoBucket(t *testing.T) {
	p, mockPuller := newPullerWithMock(t)

	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "rt:triton",
		ModelKey:  `{"storage_params":{}, "storage_key": "myStorage", "model_type": {"name": "tensorflow"}}`,
	}

	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "singlefile", "model.zip"),
		ModelType: "rt:triton",
		ModelKey:  `{"model_type":{"name":"tensorflow"},"disk_size_bytes":60}`,
	}

	expectedConfig, err := readStorageConfig("myStorage")
	assert.NoError(t, err)
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

	mockPuller.EXPECT().Pull(gomock.Any(), eqPullCommand(&expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(context.Background(), request)
	assert.Nil(t, err)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
}

func Test_ProcessLoadModelRequest_SuccessNoBucketNoStorageParams(t *testing.T) {
	p, mockPuller := newPullerWithMock(t)

	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "mt:tensorflow",
		ModelKey:  `{"storage_key": "myStorage", "model_type": {"name": "tensorflow"}}`,
	}

	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "singlefile", "model.zip"),
		ModelType: "mt:tensorflow",
		ModelKey:  `{"model_type":{"name":"tensorflow"},"disk_size_bytes":60}`,
	}

	expectedConfig, err := readStorageConfig("myStorage")
	assert.NoError(t, err)
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

	mockPuller.EXPECT().Pull(gomock.Any(), eqPullCommand(&expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(context.Background(), request)
	assert.Nil(t, err)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
}

func Test_ProcessLoadModelRequest_SuccessStorageTypeOnly(t *testing.T) {
	p, mockPuller := newPullerWithMock(t)

	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "rt:triton",
		ModelKey:  `{"storage_params": {"type": "generic"}, "model_type": {"name": "tensorflow"}}`,
	}

	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "singlefile", "model.zip"),
		ModelType: "rt:triton",
		ModelKey:  `{"model_type":{"name":"tensorflow"},"disk_size_bytes":60}`,
	}

	expectedConfig := pullman.NewRepositoryConfig("generic", nil)

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

	mockPuller.EXPECT().Pull(gomock.Any(), eqPullCommand(&expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(context.Background(), request)
	assert.Nil(t, err)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
}

func Test_ProcessLoadModelRequest_DefaultStorageKey(t *testing.T) {
	// create a typeless default storage config file for this test
	defaultConfigFile := filepath.Join(StorageConfigDir, "default")
	err := ioutil.WriteFile(defaultConfigFile, []byte(`{"type": "typeless-default"}`), 0555)
	assert.NoError(t, err)
	defer func() {
		errd := os.Remove(defaultConfigFile)
		assert.NoError(t, errd)
	}()

	p, mockPuller := newPullerWithMock(t)

	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "mt:tensorflow",
		ModelKey:  `{"model_type": {"name": "tensorflow"}}`,
	}

	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "singlefile", "model.zip"),
		ModelType: "mt:tensorflow",
		ModelKey:  `{"model_type":{"name":"tensorflow"},"disk_size_bytes":60}`,
	}

	expectedConfig, err := readStorageConfig("default")
	assert.NoError(t, err)

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

	mockPuller.EXPECT().Pull(gomock.Any(), eqPullCommand(&expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(context.Background(), request)
	assert.Nil(t, err)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
}

func Test_ProcessLoadModelRequest_DefaultStorageKeyTyped(t *testing.T) {
	p, mockPuller := newPullerWithMock(t)

	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "rt:triton",
		ModelKey:  `{"storage_params": {"type": "test-default-type"},"model_type":{"name": "tensorflow"}}`,
	}

	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "singlefile", "model.zip"),
		ModelType: "rt:triton",
		ModelKey:  `{"model_type":{"name":"tensorflow"},"disk_size_bytes":60}`,
	}

	expectedConfig, err := readStorageConfig("default_test-default-type")
	assert.NoError(t, err)

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

	mockPuller.EXPECT().Pull(gomock.Any(), eqPullCommand(&expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(context.Background(), request)
	assert.Nil(t, err)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
}

func Test_ProcessLoadModelRequest_StorageParamsOverrides(t *testing.T) {
	p, mockPuller := newPullerWithMock(t)

	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "mt:tensorflow",
		ModelKey:  `{"storage_key": "genericParameters", "storage_params": {"key": "req_value", "struct.s2_key": "req_s2_value"}, "model_type": {"name": "tensorflow"}}`,
	}

	expectedRequestRewrite := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: filepath.Join(p.PullerConfig.RootModelDir, "singlefile", "model.zip"),
		ModelType: "mt:tensorflow",
		ModelKey:  `{"model_type":{"name":"tensorflow"},"disk_size_bytes":60}`,
	}

	expectedConfig, err := readStorageConfig("genericParameters")
	assert.NoError(t, err)
	expectedConfig.Set("key", "req_value")
	expectedConfig.Set("struct", map[string]string{"s1_key": "sc_s1_value", "s2_key": "req_s2_value"})

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

	mockPuller.EXPECT().Pull(gomock.Any(), eqPullCommand(&expectedPullCommand)).Return(nil).Times(1)

	returnRequest, err := p.ProcessLoadModelRequest(context.Background(), request)
	assert.Nil(t, err)
	assert.Equal(t, expectedRequestRewrite, returnRequest)
}

func Test_ProcessLoadModelRequest_FailInvalidModelKey(t *testing.T) {
	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "rt:triton",
		ModelKey:  `{}{"model_type":{"name": "tensorflow"}}`,
	}

	p, _ := newPullerWithMock(t)

	returnRequest, err := p.ProcessLoadModelRequest(context.Background(), request)
	assert.Contains(t, err.Error(), "Invalid modelKey in LoadModelRequest")
	assert.Nil(t, returnRequest)
}

func Test_ProcessLoadModelRequest_FailInvalidSchemaPath(t *testing.T) {
	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "mt:tensorflow",
		ModelKey:  `{"model_type":{"name": "tensorflow"},"schema_path": 2}`,
	}

	p, _ := newPullerWithMock(t)

	returnRequest, err := p.ProcessLoadModelRequest(context.Background(), request)
	assert.Nil(t, returnRequest)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid modelKey in LoadModelRequest")
}

func Test_ProcessLoadModelRequest_FailMissingStorageKeyAndType(t *testing.T) {
	request := &mmesh.LoadModelRequest{
		ModelId:   "singlefile",
		ModelPath: "model.zip",
		ModelType: "rt:triton",
		ModelKey:  `{"model_type": {"name": "tensorflow"}}`,
	}
	expectedError := "Predictor Storage field missing"

	p, _ := newPullerWithMock(t)

	returnRequest, err := p.ProcessLoadModelRequest(context.Background(), request)
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
