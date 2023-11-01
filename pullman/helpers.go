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
package pullman

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
)

// OpenFile will check the path and the filesystem for mismatch errors
func OpenFile(path string) (*os.File, error) {
	// resource paths need to be compatible with a local filesystem download
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		// error path is an existing dir
		return nil, fmt.Errorf("file already exists as a directory")
	} else if os.IsNotExist(err) {
		// the path does not exist, create the parent dirs
		fileDir := filepath.Dir(path)
		if mkdirErr := os.MkdirAll(fileDir, os.ModePerm); mkdirErr != nil {
			// problem making the parent dirs, maybe somewhere in the path was an existing file
			return nil, fmt.Errorf("error creating directories: %w", mkdirErr)
		}
	} else if err != nil {
		return nil, fmt.Errorf("error trying to stat local file: %w", err)
	}

	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("os error creating file: %w", err)
	}
	return file, nil
}

// HashStrings generates a hash from the concatenation of the passed strings
// Provides a common way for providers to implement GetKey in the case that some
// configuration's values are considered secret. Use the hash as part of the key
// instead
func HashStrings(strings ...string) string {

	h := fnv.New64a()
	for _, s := range strings {
		h.Write([]byte(s))
	}

	return fmt.Sprintf("%#x", h.Sum64())
}
