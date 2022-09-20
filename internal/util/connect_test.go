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
package util

import (
	"fmt"
	"testing"
)

func TestResolveLocalGrpcEndpoint(t *testing.T) {
	testCases := []struct {
		endpoint string
		expected string
	}{
		{"port:8085", "localhost:8085"},
		{"unix:/socket/path", "unix:/socket/path"},
	}
	for _, tc := range testCases {
		got, err := ResolveLocalGrpcEndpoint(tc.endpoint)
		if err != nil {
			fmt.Println("Have err", err)
			t.Fatalf("error performing ResolveLocalGrpcEndpoint: %v", err)
		}
		if got != tc.expected {
			t.Errorf("ResolveLocalGrpcEndpoint(%v) = %v; expected %v", tc.endpoint, got, tc.expected)
		}
	}
}
