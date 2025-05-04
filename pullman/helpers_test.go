// Copyright 2022 IBM Corporation
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
	"bytes"
	"compress/gzip"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const generatedDirectory = "generated"

var zipFilePath = filepath.Join(generatedDirectory, "test-archive.zip")
var tarFilePath = filepath.Join(generatedDirectory, "test-archive.tar")
var tarGzFilePath = filepath.Join(generatedDirectory, "test-archive.tar.gz")
var log = zap.New(zap.UseDevMode(true))

var files = []struct {
	Name, Body string
}{
	{"nested/path/file1.txt", "Foo"},
	{"file2.txt", "Bar"},
	{"file3.txt", "Fun"},
}

func generateZip() {
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	for _, file := range files {
		f, err := zipWriter.Create(file.Name)
		if err != nil {
			log.Error(err, "Failed to add file to test zip file")
			os.Exit(1)
		}
		_, err = f.Write([]byte(file.Body))
		if err != nil {
			log.Error(err, "Failed to write file to test zip file")
			os.Exit(1)
		}
	}

	if err := zipWriter.Close(); err != nil {
		log.Error(err, "Failed to close zip writer")
	}

	writeBytes(buf.Bytes(), zipFilePath)
}

func generateTar() {
	buf := new(bytes.Buffer)
	tarWriter := tar.NewWriter(buf)
	defer tarWriter.Close()

	for _, file := range files {
		header := &tar.Header{
			Name: file.Name,
			Mode: 0600,
			Size: int64(len(file.Body)),
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			log.Error(err, "Failed to write header to test tar file")
			os.Exit(1)
		}
		if _, err := tarWriter.Write([]byte(file.Body)); err != nil {
			log.Error(err, "Failed to write header to test tar file")
			os.Exit(1)
		}
	}

	if err := tarWriter.Close(); err != nil {
		log.Error(err, "Failed to close tar writer")
	}

	writeBytes(buf.Bytes(), tarFilePath)
}

func generateTarGz() {
	buf := new(bytes.Buffer)
	gzipWriter := gzip.NewWriter(buf)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	for _, file := range files {
		header := &tar.Header{
			Name: file.Name,
			Mode: 0600,
			Size: int64(len(file.Body)),
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			log.Error(err, "Failed to write header to test tar.gz file")
			os.Exit(1)
		}
		if _, err := tarWriter.Write([]byte(file.Body)); err != nil {
			log.Error(err, "Failed to write header to test tar.gz file")
			os.Exit(1)
		}
	}

	if err := gzipWriter.Close(); err != nil {
		log.Error(err, "Failed to close gzip writer")
	}

	writeBytes(buf.Bytes(), tarGzFilePath)
}

func writeBytes(bytes []byte, outputPath string) {
	if err := os.MkdirAll(filepath.Dir(outputPath), os.ModePerm); err != nil {
		log.Error(err, "Failed to create archive parent directories")
		os.Exit(1)
	}

	if err := ioutil.WriteFile(outputPath, bytes, 0777); err != nil {
		log.Error(err, "Failed to write archive file to disk")
		os.Exit(1)
	}
}

func tearDown() {
	os.RemoveAll(generatedDirectory)
}

func Test_ExtractZip(t *testing.T) {
	generateZip()
	defer tearDown()

	err := ExtractZip(zipFilePath, generatedDirectory)
	assert.NoError(t, err)

	for _, file := range files {
		contents, err := os.ReadFile(filepath.Join(generatedDirectory, file.Name))
		assert.NoError(t, err)
		assert.Equal(t, file.Body, string(contents))
	}
}

func Test_ExtractTar(t *testing.T) {
	generateTar()
	defer tearDown()

	err := ExtractTar(tarFilePath, generatedDirectory)
	assert.NoError(t, err)

	for _, file := range files {
		contents, err := os.ReadFile(filepath.Join(generatedDirectory, file.Name))
		assert.NoError(t, err)
		assert.Equal(t, file.Body, string(contents))
	}

}

func Test_ExtractTarGz(t *testing.T) {
	generateTarGz()
	defer tearDown()

	newFilePath := strings.TrimSuffix(tarGzFilePath, ".gz")
	err := ExtractGzip(tarGzFilePath, newFilePath)
	assert.NoError(t, err)
	err = ExtractTar(newFilePath, generatedDirectory)
	assert.NoError(t, err)

	for _, file := range files {
		contents, err := os.ReadFile(filepath.Join(generatedDirectory, file.Name))
		assert.NoError(t, err)
		assert.Equal(t, file.Body, string(contents))
	}

}
