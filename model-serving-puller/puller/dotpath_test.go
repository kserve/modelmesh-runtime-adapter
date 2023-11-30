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
package puller

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Dotpath(t *testing.T) {
	tests := map[string]struct {
		input     string            // JSON string representing the input map
		overrides map[string]string // field overrides in a map of dotpaths to values
		want      string            // JSON string representing the output map
	}{
		"change_value": {
			input:     `{"key": "value", "another_key": "another value"}`,
			overrides: map[string]string{"key": "simple"},
			want:      `{"key": "simple", "another_key": "another value"}`,
		},
		"create_field": {
			input:     `{}`,
			overrides: map[string]string{"key": "create_me"},
			want:      `{"key": "create_me"}`,
		},
		"create_nested_object": {
			input: `{"key": "value"}`,
			overrides: map[string]string{
				"nested.object.key":     "nested value",
				"nested.object.another": "another one",
			},
			want: `{"key": "value", "nested":{"object":{"key": "nested value", "another": "another one"}}}`,
		},
		"create_field_in_nested_object": {
			input: `{"struct": {"param": "param_value"}}`,
			overrides: map[string]string{
				"struct.key": "create_me",
			},
			want: `{"struct": {"key": "create_me", "param": "param_value"}}`,
		},
		"error_no_overwrite_object": {
			input: `{"struct": {"key": "value"}}`,
			overrides: map[string]string{
				"struct": "new_value",
			},
		},
		"error_no_overwrite_array": {
			input: `{"array": ["key"], "some_other_key": "value"}`,
			overrides: map[string]string{
				"array.key": "value",
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var obj map[string]interface{}
			err := json.Unmarshal([]byte(tt.input), &obj)
			assert.NoError(t, err)

			err = ApplyParameterOverrides(obj, tt.overrides)
			// let an empty value for `want` signal that an error is expected
			if tt.want == "" {
				assert.Error(t, err)
				return
			}

			// compare results as JSON strings
			got, err := json.Marshal(obj)
			assert.NoError(t, err)

			// re-serialize `want` to remove dependence on key order
			var wobj map[string]interface{}
			err = json.Unmarshal([]byte(tt.want), &wobj)
			assert.NoError(t, err)
			want, err := json.Marshal(wobj)
			assert.NoError(t, err)

			assert.Equal(t, string(want), string(got))
		})
	}
}
