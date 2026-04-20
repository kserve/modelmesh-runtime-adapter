// Copyright 2021 IBM Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package httpprovider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/kserve/modelmesh-runtime-adapter/pullman"
)

type httpClientFactory struct{}

// httpClientFactory implements fetcherFactory
var _ fetcherFactory = (*httpClientFactory)(nil)

func (f httpClientFactory) newClient(log logr.Logger, ca *x509.CertPool, client_tls *tls.Certificate) fetcher {

	// net/http provides a default Transport and Client, but the settings
	// are not conducive to production use so we create our own here
	//  eg. the default client has no request timeout
	t := http.Transport{
		MaxIdleConns:        100,
		MaxConnsPerHost:     100,
		MaxIdleConnsPerHost: 100,
	}
	if ca != nil {
		t.TLSClientConfig.RootCAs = ca
	}
	if client_tls != nil {
		t.TLSClientConfig.Certificates = []tls.Certificate{*client_tls}
	}

	return &httpFetcher{
		httpClient: &http.Client{
			Transport: &t,
			Timeout:   15 * time.Minute,
		},
		log: log,
	}

}

type httpFetcher struct {
	httpClient *http.Client

	log logr.Logger
}

// httpFetcher implements fetcher
var _ fetcher = (*httpFetcher)(nil)

func (c *httpFetcher) download(ctx context.Context, req *http.Request, filename string) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error getting resource '%s': %w", req.URL.String(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("error getting resource '%s'. HTTP Status Code: %d, Error: %w", req.URL.String(), resp.StatusCode, readErr)
		}
		return fmt.Errorf("error getting resource '%s'. HTTP Status Code: %d, Error: %s", req.URL.String(), resp.StatusCode, string(body))
	}

	// Download file first.
	file, fileErr := pullman.OpenFile(filename)
	if fileErr != nil {
		return fmt.Errorf("unable to open local file '%s' for writing: %w", filename, fileErr)
	}
	defer file.Close()

	if _, err = io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("error writing resource to local file '%s': %w", filename, err)
	}

	fileFormat := pullman.GetFileFormat(filename)

	// Check if the file format is a supported archive format and automatically extract it.
	if fileFormat != nil {
		switch fileFormat.Extension {
		case "gz":
			decompressedFilePath := strings.TrimSuffix(filename, ".gz")
			if err := pullman.ExtractGzip(filename, decompressedFilePath); err != nil {
				return fmt.Errorf("error decompressing gzip file '%s': %w", filename, err)
			}
			os.Remove(filename)
			fileFormat = pullman.GetFileFormat(decompressedFilePath)
			if fileFormat != nil && fileFormat.Extension == "tar" {
				if err := pullman.ExtractTar(decompressedFilePath, filepath.Dir(decompressedFilePath)); err != nil {
					return fmt.Errorf("error extracting tar file '%s': %w", filename, err)
				}
				os.Remove(decompressedFilePath)
			}
		case "tar":
			if err := pullman.ExtractTar(filename, filepath.Dir(filename)); err != nil {
				return fmt.Errorf("error extracting tar file '%s': %w", filename, err)
			}
			os.Remove(filename)
		case "zip":
			if err := pullman.ExtractZip(filename, filepath.Dir(filename)); err != nil {
				return fmt.Errorf("error extracting zip file '%s': %w", filename, err)
			}
			os.Remove(filename)
		}
	}
	return nil
}
