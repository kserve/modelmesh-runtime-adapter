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
	"fmt"
	"strings"
)

// Implements a simple "dotpath" style for setting values within JSON compatible configuration structs
//
// To keep things simple:
// - only string values are supported
// - paths can only trace object keys (not arrays)
// - only values that are strings can be overwritten
func ApplyParameterOverrides(params map[string]interface{}, overrides map[string]string) error {
	for dotpath, value := range overrides {
		if err := set(params, dotpath, value); err != nil {
			return err
		}
	}
	return nil
}

func fieldsFromDotpath(dotpath string) []string {
	return strings.Split(dotpath, ".")
}

func set(params map[string]interface{}, dotpath string, value string) error {
	if params == nil {
		return fmt.Errorf("got nil map, unable to set value")
	}

	fields := fieldsFromDotpath(dotpath)
	if len(fields) == 0 {
		return nil
	}

	for i, field := range fields[:len(fields)-1] {
		if obj, ok := params[field]; !ok {
			next := map[string]interface{}{}
			params[field] = next
			params = next
		} else if params, ok = obj.(map[string]interface{}); !ok {
			return fmt.Errorf("expected an object at '%s'", strings.Join(fields[:i], "."))
		}
	}

	lastField := fields[len(fields)-1]
	// return an error if overwriting an existing value that is not a string
	if v, ok := params[lastField]; ok {
		if _, ok = v.(string); !ok {
			return fmt.Errorf("expected a string at '%s', but got %v", strings.Join(fields, "."), params[lastField])
		}
	}
	params[lastField] = value

	return nil
}
