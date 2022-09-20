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
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// RemoveFileFromListOfFileInfo
// The input `files` content is modified, the order of elements is changed
func RemoveFileFromListOfFileInfo(filename string, files []os.FileInfo) (bool, []os.FileInfo) {
	var fileIndex int = -1
	for i, f := range files {
		if f.Name() == filename {
			fileIndex = i
		}
	}
	if fileIndex == -1 {
		return false, files
	}
	// overwrite the entry to be removed with the last entry
	files[fileIndex] = files[len(files)-1]
	// then return a shortend slice
	return true, files[:len(files)-1]
}

// Check if a file exists at path
func FileExists(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, err
	}
}

// Clear contents of directory if it exists.
// If condition is specified then only delete dir entries to which it returns true
func ClearDirectoryContents(dirPath string, condition func(entry os.DirEntry) bool) error {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // ok
		}
		return fmt.Errorf("Error listing files to clean up in model dir %s: %w", dirPath, err)
	}
	for _, f := range files {
		if condition != nil && !condition(f) {
			continue
		}
		filePath := filepath.Join(dirPath, f.Name()) // this is safe, securejoin not needed
		if err = os.RemoveAll(filePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("Error removing preexisting entry from model store dir: %s: %w", filePath, err)
		}
	}
	return nil
}
