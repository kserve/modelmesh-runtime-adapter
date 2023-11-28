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

package s3provider

import (
	"testing"

	"github.com/IBM/ibm-cos-sdk-go/service/s3"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func Test_shouldIgnoreObject(t *testing.T) {
	log := zap.New()
	downloader := ibmS3Downloader{
		log: log,
	}
	tableTests := []struct {
		objSize      int64
		objKey       string
		prefix       string
		shouldIgnore bool
	}{
		// ingore an empty object
		{
			objSize:      0,
			shouldIgnore: true,
		},
		// ignore an object with a directory-like path (need to stay compatible with file-system paths)
		{
			objSize:      1,
			objKey:       "path/",
			prefix:       "path",
			shouldIgnore: true,
		},
		// only match key extension on prefix if separated with a slash
		{
			objSize:      1,
			objKey:       "path/obj",
			prefix:       "path",
			shouldIgnore: false,
		},
		{
			objSize:      1,
			objKey:       "path/obj",
			prefix:       "path/",
			shouldIgnore: false,
		},
		{
			objSize:      1,
			objKey:       "path_with_more",
			prefix:       "path",
			shouldIgnore: true,
		},
		{
			objSize:      1,
			objKey:       "path_with_more/obj",
			prefix:       "path",
			shouldIgnore: true,
		},
	}

	for _, tt := range tableTests {
		t.Run("", func(t *testing.T) {
			obj := s3.Object{
				Size: &tt.objSize,
				Key:  &tt.objKey,
			}
			res := downloader.shouldIgnoreObject(&obj, tt.prefix)

			if tt.shouldIgnore && !res {
				t.Errorf("object should have been ignored but it should have passed")
			}
			if !tt.shouldIgnore && res {
				t.Errorf("object should have passed but it was ignored")
			}
		})
	}
}
