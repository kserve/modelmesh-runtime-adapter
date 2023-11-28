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
package util

import (
	"fmt"
	"path/filepath"

	securejoin "github.com/cyphar/filepath-securejoin"
)

func SecureJoin(elem ...string) (string, error) {
	if len(elem) > 2 {
		str := filepath.Join(elem[1:]...)
		return securejoin.SecureJoin(elem[0], str)
	} else if len(elem) == 2 {
		return securejoin.SecureJoin(elem[0], elem[1])
	} else {
		return "", fmt.Errorf("Expected at least 2 parameters")
	}
}
