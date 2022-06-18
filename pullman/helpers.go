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
package pullman

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type FileFormat struct {
	MagicBytes []byte
	Offset     int
	Extension  string
}

// Magic byte values pulled from: https://en.wikipedia.org/wiki/List_of_file_signatures
var fileFormats = []FileFormat{
	{
		MagicBytes: []byte{0x75, 0x73, 0x74, 0x61, 0x72, 0x00, 0x30, 0x30},
		Offset:     257,
		Extension:  "tar",
	},
	{
		MagicBytes: []byte{0x75, 0x73, 0x74, 0x61, 0x72, 0x20, 0x20, 0x00},
		Offset:     257,
		Extension:  "tar",
	},
	{
		MagicBytes: []byte{0x1F, 0x8B},
		Offset:     0,
		Extension:  "gz",
	},
	{
		MagicBytes: []byte{0x50, 0x4B, 0x03, 0x04},
		Offset:     0,
		Extension:  "zip",
	},

	{
		MagicBytes: []byte{0x50, 0x4B, 0x05, 0x06},
		Offset:     0,
		Extension:  "zip",
	},

	{
		MagicBytes: []byte{0x50, 0x4B, 0x07, 0x08},
		Offset:     0,
		Extension:  "zip",
	},
}

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

// Extract a zip file into the provided destination directory.
func ExtractZip(filePath string, dest string) error {
	zipReader, err := zip.OpenReader(filePath)
	if err != nil {
		return fmt.Errorf("unable to open '%s' for reading: %w", filePath, err)
	}
	defer zipReader.Close()

	prefix := filepath.Clean(dest) + string(os.PathSeparator)
	for _, zipFileEntry := range zipReader.File {
		destFilePath := filepath.Join(dest, zipFileEntry.Name)

		// Zip slip vulnerability check
		if !strings.HasPrefix(destFilePath, prefix) {
			return fmt.Errorf("%s: illegal file path", destFilePath)
		}

		if zipFileEntry.FileInfo().IsDir() {
			err = os.MkdirAll(destFilePath, 0755)
			if err != nil {
				return fmt.Errorf("error creating new directory %s", destFilePath)
			}
			continue
		}

		file, fileErr := OpenFile(destFilePath)
		if fileErr != nil {
			return fmt.Errorf("unable to open local file '%s' for writing: %w", destFilePath, fileErr)
		}
		defer file.Close()

		zippedRc, err := zipFileEntry.Open()
		if err != nil {
			return fmt.Errorf("error opening zip file entry: %w", err)
		}
		defer zippedRc.Close()

		if _, err = io.Copy(file, zippedRc); err != nil {
			return fmt.Errorf("error writing zip resource to local file '%s': %w", destFilePath, err)
		}

	}
	return nil
}

// Extract a tar archive file into the provided destination directory.
func ExtractTar(filePath string, dest string) error {
	tarFile, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("unable to open '%s' for reading: %w", filePath, err)
	}
	defer tarFile.Close()

	tr := tar.NewReader(tarFile)
	for {
		header, err := tr.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			return fmt.Errorf("error reading tar archive entry: %w", err)
		}

		if header == nil {
			continue
		}

		destFilePath := filepath.Join(dest, header.Name)
		if header.Typeflag == tar.TypeDir {
			err = os.MkdirAll(destFilePath, 0755)
			if err != nil {
				return fmt.Errorf("error creating new directory %s", destFilePath)
			}
			continue
		}

		file, fileErr := OpenFile(destFilePath)
		if fileErr != nil {
			return fmt.Errorf("unable to open local file '%s' for writing: %w", destFilePath, fileErr)
		}
		defer file.Close()
		if _, err = io.Copy(file, tr); err != nil {
			return fmt.Errorf("error writing tar resource to local file '%s': %w", destFilePath, err)
		}
	}
	return nil
}

// Extract a gzip compressed file into the provided destination file path.
func ExtractGzip(filePath string, dest string) error {
	gzipFile, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("unable to open '%s' for reading: %w", filePath, err)
	}
	defer gzipFile.Close()
	gzr, err := gzip.NewReader(gzipFile)
	if err != nil {
		return fmt.Errorf("unable to create gzip reader: %w", err)
	}
	defer gzr.Close()

	file, fileErr := OpenFile(dest)
	if fileErr != nil {
		return fmt.Errorf("unable to open local file '%s' for writing: %w", dest, fileErr)
	}
	defer file.Close()

	if _, err = io.Copy(file, gzr); err != nil {
		return fmt.Errorf("error writing gzip resource to local file '%s': %w", dest, err)
	}

	return nil
}

// Get the file type based on the first few hundred bytes of the stream.
// If the file isn't one of the expected formats, nil is returned.
// If an error occurs while determining the file format, nil is returned.
func GetFileFormat(filePath string) *FileFormat {

	file, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer file.Close()

	r := bufio.NewReader(file)

	// Due to the tar magic bytes offset, this is the minimum number of bytes we need to read.
	numBytes := 264
	fileBytes, err := r.Peek(numBytes)
	if err != nil {
		return nil
	}

	for _, format := range fileFormats {
		if bytes.Equal(fileBytes[format.Offset:format.Offset+len(format.MagicBytes)], format.MagicBytes) {
			return &format
		}
	}
	return nil
}
