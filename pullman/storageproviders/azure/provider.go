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
package azureprovider

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
	"github.com/kserve/modelmesh-runtime-adapter/pullman"
)

const (
	// For authentication with Azure Service Principal
	configClientId     = "client_id"
	configClientSecret = "client_secret"
	configTenantId     = "tenant_id"

	// For authentication with a connection string
	configConnectionString = "connection_string"

	// Other config fields
	configAccountName = "account_name"
	configContainer   = "container"

	azureBlobStorageURL    = ".blob.core.windows.net/"
	maxDownloadConcurrency = 4
)

// azureDownloaderFactory is the interface used create Azure Blob Storage downloaders
// useful to mock for testing
type azureDownloaderFactory interface {
	newDownloaderWithNoCredential(log logr.Logger, containerUrl string) (azureDownloader, error)
	newDownloaderWithConnectionString(log logr.Logger, containerName string, connectionString string) (azureDownloader, error)
	newDownloaderWithServicePrincipal(log logr.Logger, containerUrl string, credentials servicePrincipalCredentials) (azureDownloader, error)
}

// azureDownloader is the interface used to download resources from Azure Blob Storage
// useful to mock for testing
type azureDownloader interface {
	listObjects(ctx context.Context, prefix string) ([]string, error)
	downloadBatch(ctx context.Context, targets []pullman.Target) error
}

type azureProvider struct {
	azureDownloaderFactory azureDownloaderFactory
}

// azureProvider implements StorageProvider
var _ pullman.StorageProvider = (*azureProvider)(nil)

func (p azureProvider) GetKey(config pullman.Config) string {
	// hash the values of all configurations that go into creating the client
	// no need to validate the config here
	clientId, _ := pullman.GetString(config, configClientId)
	clientSecret, _ := pullman.GetString(config, configClientSecret)
	tenantId, _ := pullman.GetString(config, configTenantId)
	connectionString, _ := pullman.GetString(config, configConnectionString)
	accountName, _ := pullman.GetString(config, configAccountName)
	container, _ := pullman.GetString(config, configContainer)

	return pullman.HashStrings(clientId, clientSecret, tenantId, connectionString, accountName, container)
}

func (p azureProvider) NewRepository(config pullman.Config, log logr.Logger) (pullman.RepositoryClient, error) {

	clientId, _ := pullman.GetString(config, configClientId)
	clientSecret, _ := pullman.GetString(config, configClientSecret)
	tenantId, _ := pullman.GetString(config, configTenantId)
	connectionString, _ := pullman.GetString(config, configConnectionString)
	accountName, _ := pullman.GetString(config, configAccountName)
	container, _ := pullman.GetString(config, configContainer)

	if accountName == "" || container == "" {
		return nil, errors.New("both the Azure account name and storage container name must be specified")
	}

	var err error
	var azclient azureDownloader
	containerUrl := "https://" + accountName + azureBlobStorageURL + container
	if connectionString != "" {
		// If connection string is provided, use that.
		azclient, err = p.azureDownloaderFactory.newDownloaderWithConnectionString(log, container, connectionString)
	} else if clientId != "" && clientSecret != "" && tenantId != "" {
		// If service principal credentials were provided, use that.
		spc := servicePrincipalCredentials{
			clientId:     clientId,
			clientSecret: clientSecret,
			tenantId:     tenantId,
		}
		azclient, err = p.azureDownloaderFactory.newDownloaderWithServicePrincipal(log, containerUrl, spc)
	} else {
		// Otherwise use no authentication.
		azclient, err = p.azureDownloaderFactory.newDownloaderWithNoCredential(log, containerUrl)
	}

	if err != nil {
		return nil, err
	}
	return &azureRepositoryClient{
		azclient: azclient,
		log:      log,
	}, nil
}

type servicePrincipalCredentials struct {
	clientId     string
	clientSecret string
	tenantId     string
}

type azureRepositoryClient struct {
	azclient azureDownloader
	log      logr.Logger
}

func (r *azureRepositoryClient) Pull(ctx context.Context, pc pullman.PullCommand) error {
	destDir := pc.Directory
	targets := pc.Targets

	// Process per-command configuration
	container, ok := pullman.GetString(pc.RepositoryConfig, configContainer)
	if !ok {
		return fmt.Errorf("required configuration '%s' missing from command", configContainer)
	}

	// Resolve full paths of objects to download and local paths for the resulting files
	// Mainly, this means resolving the objects referenced by a "directory" in Azure.
	resolvedTargets := make([]pullman.Target, 0, len(targets))
	for _, pt := range targets {
		objPaths, err := r.azclient.listObjects(ctx, pt.RemotePath)

		if err != nil {
			return fmt.Errorf("unable to list objects in container '%s': %w", container, err)
		}
		r.log.V(1).Info("found objects to download", "path", pt.RemotePath, "count", len(objPaths))

		for _, objPath := range objPaths {
			localPath := pt.LocalPath
			relativePath := strings.TrimPrefix(objPath, pt.RemotePath)

			// handle case where the remote path is a single object
			if relativePath == "" {
				// allow renaming of the file
				if localPath != "" && !strings.HasSuffix(localPath, "/") {
					relativePath = path.Base(localPath)
					localPath = path.Dir(localPath)
				} else {
					relativePath = path.Base(objPath)
				}
			}

			filePath, joinErr := util.SecureJoin(destDir, localPath, relativePath)
			if joinErr != nil {
				return fmt.Errorf("error joining filepaths '%s' and '%s': %w", pt.LocalPath, relativePath, joinErr)
			}

			t := pullman.Target{
				RemotePath: objPath,
				LocalPath:  filePath,
			}
			resolvedTargets = append(resolvedTargets, t)
		}
	}

	if err := r.azclient.downloadBatch(ctx, resolvedTargets); err != nil {
		return fmt.Errorf("unable to download objects in container '%s': %w", container, err)
	}

	return nil

}

func init() {
	p := azureProvider{
		azureDownloaderFactory: azureClientFactory{},
	}
	pullman.RegisterProvider("azure", p)
}
