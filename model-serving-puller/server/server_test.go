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
package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	gomock "github.com/golang/mock/gomock"
	"github.com/joho/godotenv"

	"github.com/kserve/modelmesh-runtime-adapter/internal/proto/mmesh"
	"github.com/kserve/modelmesh-runtime-adapter/model-serving-puller/generated/mocks"
	. "github.com/kserve/modelmesh-runtime-adapter/model-serving-puller/puller"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	TestDataDir      = "../testdata"
	StorageConfigDir = filepath.Join(TestDataDir, "/storage-config")
	RootModelDir     = filepath.Join(TestDataDir, "/models")
)

func newPullerServerWithMocks(t *testing.T) (*PullerServer, *mocks.MockModelRuntimeClient, *mocks.MockS3Downloader, *gomock.Controller) {
	log := zap.New()
	err := godotenv.Load("../.env")
	if err != nil {
		log.Info("Did not load .env file ", "error", err)
	}

	mockCtrl := gomock.NewController(t)
	mockClient := mocks.NewMockModelRuntimeClient(mockCtrl)
	mockDownloader := mocks.NewMockS3Downloader(mockCtrl)

	rootModelDir, err := filepath.Abs(RootModelDir)
	if err != nil {
		t.Fatalf("Failed to get absolute path of testdata dir %v", err)
	}

	pc := PullerConfiguration{
		RootModelDir:            rootModelDir,
		StorageConfigurationDir: StorageConfigDir,
		S3DownloadConcurrency:   0,
	}
	p := NewPullerFromConfig(log, &pc)
	p.Log = log
	// inject mock downloader
	p.NewS3DownloaderFromConfig = func(*StorageConfiguration, int, logr.Logger) (S3Downloader, error) { return mockDownloader, nil }

	s := NewPullerServer(log)
	s.modelRuntimeClient = mockClient
	s.puller = p
	s.Log = log

	return s, mockClient, mockDownloader, mockCtrl
}

func TestLoadModel(t *testing.T) {
	var loadModelTests = []struct {
		modelID           string
		inputModelPath    string
		cloudStorageFiles []string
		outputModelPath   string
	}{
		{"testmodel", "model.zip", []string{"model.zip"}, "testmodel/model.zip"},
		{"testmodel", "model.zip", []string{"model.zip"}, "testmodel/model.zip"},
		{"testmodel", "models/app1/model.zip", []string{"models/app1/model.zip"}, "testmodel/model.zip"},
		{"testmodel", "models/app1", []string{"models/app1/model.zip", "models/app1/metadata.txt"}, "testmodel"},
	}

	for _, tt := range loadModelTests {
		t.Run("", func(t *testing.T) {
			s, mockClient, mockDownloader, mockCtrl := newPullerServerWithMocks(t)
			defer mockCtrl.Finish()

			request := &mmesh.LoadModelRequest{
				ModelId:   tt.modelID,
				ModelPath: tt.inputModelPath,
				ModelType: "tensorflow",
				ModelKey:  `{"storage_key": "myStorage", "bucket": "bucket1"}`,
			}

			expectedRequestRewrite := &mmesh.LoadModelRequest{
				ModelId:   tt.modelID,
				ModelPath: filepath.Join(s.puller.PullerConfig.RootModelDir, tt.outputModelPath),
				ModelType: "tensorflow",
				ModelKey:  `{"bucket":"bucket1","disk_size_bytes":0,"storage_key":"myStorage","storage_params":{"bucket":"bucket1"}}`,
			}

			// Assert s.LoadModel calls the s3 Download and then the model runtime LoadModel rpc
			mockDownloader.EXPECT().ListObjectsUnderPrefix("bucket1", tt.inputModelPath).Return(tt.cloudStorageFiles, nil).Times(1)
			mockDownloader.EXPECT().DownloadWithIterator(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockClient.EXPECT().LoadModel(gomock.Any(), gomock.Eq(expectedRequestRewrite)).Return(nil, nil).Times(1)
			// mockClient.EXPECT().UnloadModel(gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err := s.LoadModel(ctx, request)

			// Assert s.LoadModel didn't return an error
			if err != nil {
				t.Errorf("Unexpected error from LoadModel: %v", err)
			}

			// TODO assert that the model file was created at the correct path
			// TODO assert that the new filepath is passed to the runtime LoadModel rpc call
		})
	}
}
