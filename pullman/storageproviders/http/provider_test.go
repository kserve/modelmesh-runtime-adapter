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

package httpprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	"github.com/kserve/modelmesh-runtime-adapter/pullman"
	"github.com/stretchr/testify/assert"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func newTestMocks(t *testing.T) (*httpProvider, *MockfetcherFactory, *httpRepository, *Mockfetcher, logr.Logger) {
	ctrl := gomock.NewController(t)

	hcf := NewMockfetcherFactory(ctrl)
	hp := &httpProvider{
		fetcherFactory: hcf,
	}

	mf := NewMockfetcher(ctrl)

	log := zap.New()
	hr := &httpRepository{
		client: mf,
		log:    log,
	}

	return hp, hcf, hr, mf, log
}

// custom GoMock matcher for an HTTP Request
type httpRequestMatcher struct {
	method string
	url    *url.URL
}

func newHttpRequestMatcher(m string, urls string) *httpRequestMatcher {
	u, err := url.Parse(urls)
	if err != nil {
		panic(err)
	}
	return &httpRequestMatcher{
		method: m,
		url:    u,
	}

}

func (hrm *httpRequestMatcher) Matches(x interface{}) bool {
	httpReq, ok := x.(*http.Request)
	if !ok || httpReq == nil {
		return false
	}

	if httpReq.Method != hrm.method {
		return false
	}

	if !reflect.DeepEqual(httpReq.URL, hrm.url) {
		return false
	}

	return true
}

func (hrm *httpRequestMatcher) String() string {
	return fmt.Sprintf("is http.Request for %s %s", hrm.method, hrm.url.String())
}

func Test_Download_SimpleFile(t *testing.T) {
	_, _, testRepo, mockClient, _ := newTestMocks(t)

	testURL := "http://someurl:8080"
	c := pullman.NewRepositoryConfig("http", nil)
	c.Set("url", testURL)

	downloadDir := filepath.Join("test", "output")
	inputPullCommand := pullman.PullCommand{
		RepositoryConfig: c,
		Directory:        downloadDir,
		Targets: []pullman.Target{
			{
				RemotePath: "models/some_model_file",
			},
		},
	}

	expectedURL := testURL + "/models/some_model_file"
	expectedFile := filepath.Join(downloadDir, "some_model_file")
	mockClient.EXPECT().download(gomock.Any(), newHttpRequestMatcher("GET", expectedURL), gomock.Eq(expectedFile)).
		Return(nil).
		Times(1)

	err := testRepo.Pull(context.Background(), inputPullCommand)
	assert.NoError(t, err)
}

func Test_Download_RenameFile(t *testing.T) {
	_, _, testRepo, mockClient, _ := newTestMocks(t)

	testURL := "http://someurl:8080"
	c := pullman.NewRepositoryConfig("http", nil)
	c.Set("url", testURL)

	downloadDir := filepath.Join("test", "output")
	inputPullCommand := pullman.PullCommand{
		RepositoryConfig: c,
		Directory:        downloadDir,
		Targets: []pullman.Target{
			{
				RemotePath: "models/some_model_file",
				LocalPath:  "local/path/my-model",
			},
		},
	}

	expectedURL := testURL + "/models/some_model_file"
	expectedFile := filepath.Join(downloadDir, "local", "path", "my-model")
	mockClient.EXPECT().download(gomock.Any(), newHttpRequestMatcher("GET", expectedURL), gomock.Eq(expectedFile)).
		Return(nil).
		Times(1)

	err := testRepo.Pull(context.Background(), inputPullCommand)
	assert.NoError(t, err)
}

func Test_GetKey(t *testing.T) {
	provider := httpProvider{}

	createTestConfig := func() *pullman.RepositoryConfig {
		config := pullman.NewRepositoryConfig("http", nil)
		config.Set(configURL, "http://some.url")
		config.Set(configHeaders, map[string]string{"header": "value"})
		config.Set(configCertificate, "<certificate>")
		config.Set(configClientCertificate, "<client_certificate>")
		config.Set(configClientKey, "<client_key>")
		return config
	}

	// should return the same result given the same config
	t.Run("shouldMatchForSameConfig", func(t *testing.T) {
		config1 := createTestConfig()
		config2 := createTestConfig()

		assert.Equal(t, provider.GetKey(config1), provider.GetKey(config2))
	})

	// changing the certificate should change the key
	t.Run("shouldChangeForCertificate", func(t *testing.T) {
		config1 := createTestConfig()
		config2 := createTestConfig()
		config2.Set(configCertificate, "<different certificate data>")

		assert.NotEqual(t, provider.GetKey(config1), provider.GetKey(config2))
	})

	// changing the headers should NOT change the key
	t.Run("shouldNotChangeForHeaders", func(t *testing.T) {
		config1 := createTestConfig()
		config2 := createTestConfig()
		config2.Set(configHeaders, map[string]string{"header": "anothervalue;"})

		assert.Equal(t, provider.GetKey(config1), provider.GetKey(config2))
	})
}
