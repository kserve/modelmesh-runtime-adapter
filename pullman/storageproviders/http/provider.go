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

package httpprovider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"

	"github.com/go-logr/logr"

	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
	"github.com/kserve/modelmesh-runtime-adapter/pullman"
)

const (
	configURL               = "url"
	configHeaders           = "headers"
	configCertificate       = "certificate"
	configClientCertificate = "client_certificate"
	configClientKey         = "client_key"
)

// interfaces

// fetcherFactory is the interface used create clients
// useful to mock for testing
type fetcherFactory interface {
	newClient(log logr.Logger, ca *x509.CertPool, client_tls *tls.Certificate) fetcher
}

type fetcher interface {
	download(ctx context.Context, req *http.Request, filename string) error
}

// structs

type httpProvider struct {
	fetcherFactory
}

// httpProvider implements StorageProvider
var _ pullman.StorageProvider = (*httpProvider)(nil)

func (p httpProvider) GetKey(config pullman.Config) string {
	// the TLS config goes into the transport, so changes to that require a new client
	// everything else is handled per Pull()
	cert, _ := pullman.GetString(config, configCertificate)
	clientCert, _ := pullman.GetString(config, configClientCertificate)
	clientKey, _ := pullman.GetString(config, configClientKey)

	return pullman.HashStrings(cert, clientCert, clientKey)
}

func (p httpProvider) NewRepository(config pullman.Config, log logr.Logger) (pullman.RepositoryClient, error) {
	// certificates are optional
	cert, _ := pullman.GetString(config, configCertificate)
	var ca *x509.CertPool
	if cert != "" {
		ca = x509.NewCertPool()
		if ok := ca.AppendCertsFromPEM([]byte(cert)); !ok {
			// TODO: would be better to surface the reason for any
			// failure in processing the cert instead of just OK if
			// at least one cert is added to the pool
			return nil, errors.New("failed to add certificate to CA pool")
		}
	}

	clientCert, _ := pullman.GetString(config, configClientCertificate)
	clientKey, _ := pullman.GetString(config, configClientKey)

	// ensure both key and cert are specified for client auth if either one exists
	if clientCert == "" && clientKey != "" {
		return nil, errors.New("missing required client certificate when client key is specified")
	}
	if clientCert != "" && clientKey == "" {
		return nil, errors.New("missing required client key when client certificate is specified")
	}

	var client_tls *tls.Certificate
	if clientCert != "" && clientKey != "" {
		c, err := tls.X509KeyPair([]byte(clientCert), []byte(clientKey))
		if err != nil {
			return nil, fmt.Errorf("failed to parse client TLS key pair: %w", err)
		}
		client_tls = &c
	}

	return &httpRepository{
		client: p.fetcherFactory.newClient(log, ca, client_tls),
		log:    log,
	}, nil
}

type httpRepository struct {
	client fetcher
	log    logr.Logger
}

// httpRepository implements RepositoryClient
var _ pullman.RepositoryClient = (*httpRepository)(nil)

func (r *httpRepository) Pull(ctx context.Context, pc pullman.PullCommand) error {
	destDir := pc.Directory
	targets := pc.Targets

	// process per-command configuration
	pcURL, ok := pullman.GetString(pc.RepositoryConfig, configURL)
	if !ok {
		return errors.New("required configuration 'url' missing from command")
	}
	u, err := url.Parse(pcURL)
	if err != nil {
		return fmt.Errorf("failed to parse url: %w", err)
	}
	baseURL := *u

	// header is optional
	var header http.Header
	if pcHeaders, ok := pc.RepositoryConfig.Get(configHeaders); ok {
		mm, errH := parseStringMultiMap(pcHeaders)
		if errH != nil {
			return fmt.Errorf("could not parse '%v' as HTTP headers: %v", pcHeaders, errH)
		}

		header = http.Header(mm)
	}

	for _, pt := range targets {
		// construct the request
		u := baseURL
		u.Path = path.Join(baseURL.Path, pt.RemotePath)

		req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if err != nil {
			return fmt.Errorf("error building HTTP request for '%s': %w", pt.RemotePath, err)
		}
		req.Header = header

		// construct the LocalPath
		localPath := pt.LocalPath
		// handle default value for LocalPath
		if localPath == "" {
			localPath = path.Base(pt.RemotePath)
		}

		filePath, joinErr := util.SecureJoin(destDir, localPath)
		if joinErr != nil {
			return fmt.Errorf("error joining filepaths '%s' and '%s': %w", destDir, localPath, joinErr)
		}
		r.log.V(1).Info("constructed local path to download file", "local", filePath, "remote", pt.RemotePath)

		downloadErr := r.client.download(ctx, req, filePath)
		if downloadErr != nil {
			return fmt.Errorf("unable to download file from '%s': %w", baseURL.String(), downloadErr)
		}
	}

	return nil
}

// parseStringMultiMap converts a JSON compatible object to a string multi-map
// The value for a key in the object can be either a single value, which is
// wrapped into a slice, or a JSON array.
func parseStringMultiMap(in interface{}) (map[string][]string, error) {

	vMap, mapOK := in.(map[string]interface{})
	if !mapOK {
		return nil, fmt.Errorf("could not parse '%v' as a map", in)
	}

	result := make(map[string][]string)
	for vKey, vVals := range vMap {
		// allow the value to be a string or a list of strings
		if vSlice, sliceOK := vVals.([]interface{}); sliceOK {
			for _, v := range vSlice {
				vString, stringOK := v.(string)
				if !stringOK {
					return nil, fmt.Errorf("could not parse '%v' as string", v)
				}
				result[vKey] = append(result[vKey], vString)
			}
		} else if vString, stringOK := vVals.(string); stringOK {
			result[vKey] = append(result[vKey], vString)
		} else {
			return nil, fmt.Errorf("could not parse '%v' as a string or a list of strings", vVals)
		}
	}

	return result, nil
}

func init() {
	p := httpProvider{
		fetcherFactory: httpClientFactory{},
	}
	pullman.RegisterProvider("http", p)
}
