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
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/go-logr/logr"
	"github.com/kserve/modelmesh-runtime-adapter/pullman"
)

type azureClientFactory struct{}

// azureClientFactory implements azureDownloaderFactory
var _ azureDownloaderFactory = (*azureClientFactory)(nil)

// azureImplDownloader implements azureDownloader
var _ azureDownloader = (*azureImplDownloader)(nil)

type azureImplDownloader struct {
	client *azblob.ContainerClient
	log    logr.Logger
}

func (f azureClientFactory) newDownloaderWithNoCredential(log logr.Logger, containerUrl string) (azureDownloader, error) {
	containerClient, err := azblob.NewContainerClientWithNoCredential(containerUrl, nil)
	if err != nil {
		return nil, err
	}
	return &azureImplDownloader{
		client: &containerClient,
		log:    log,
	}, nil
}

func (f azureClientFactory) newDownloaderWithConnectionString(log logr.Logger, containerName string, connectionString string) (azureDownloader, error) {
	containerClient, err := azblob.NewContainerClientFromConnectionString(connectionString, containerName, nil)
	if err != nil {
		return nil, err
	}
	return &azureImplDownloader{
		client: &containerClient,
		log:    log,
	}, nil
}

func (f azureClientFactory) newDownloaderWithServicePrincipal(log logr.Logger, containerUrl string, credentials servicePrincipalCredentials) (azureDownloader, error) {
	cred, err := azidentity.NewClientSecretCredential(credentials.tenantId, credentials.clientId, credentials.clientSecret, nil)
	if err != nil {
		return nil, err
	}
	containerClient, err := azblob.NewContainerClient(containerUrl, cred, nil)
	if err != nil {
		return nil, err
	}
	return &azureImplDownloader{
		client: &containerClient,
		log:    log,
	}, nil
}

func (d *azureImplDownloader) listObjects(ctx context.Context, prefix string) ([]string, error) {
	pager := d.client.ListBlobsFlat(&azblob.ContainerListBlobFlatSegmentOptions{
		Prefix: &prefix,
	})
	if pager == nil {
		return nil, fmt.Errorf("listObjects: nothing returned when listing Azure blobs with prefix %s", prefix)
	}
	if pager.Err() != nil {
		return nil, pager.Err()
	}
	blobList := make([]string, 0, 10)
	for pager.NextPage(context.Background()) {
		res := pager.PageResponse()
		for _, blob := range res.Segment.BlobItems {
			if !d.shouldIgnoreObject(blob, prefix) {
				blobList = append(blobList, *blob.Name)
			}
		}
	}
	return blobList, nil
}

func (d *azureImplDownloader) downloadBatch(ctx context.Context, targets []pullman.Target) error {

	for _, target := range targets {
		file, fileErr := pullman.OpenFile(target.LocalPath)
		if fileErr != nil {
			return fmt.Errorf("unable to open local file '%s' for writing: %w", target.LocalPath, fileErr)
		}
		defer file.Close()
		d.log.V(1).Info("downloading blob", "path", target.RemotePath, "filename", target.LocalPath)
		blobClient := d.client.NewBlobClient(target.RemotePath)

		err := blobClient.DownloadBlobToFile(ctx, 0, 0, file, azblob.HighLevelDownloadFromBlobOptions{
			Parallelism: maxDownloadConcurrency,
		})
		if err != nil {
			return fmt.Errorf("unable to download blob '%s' to local file '%s' for writing: %w", target.RemotePath, target.LocalPath, err)
		}
	}
	return nil
}

func (d *azureImplDownloader) shouldIgnoreObject(blob *azblob.BlobItemInternal, prefix string) bool {
	if *blob.Properties.ContentLength == 0 {
		d.log.V(1).Info("ignore downloading azure object of 0 byte size", "azure_path", blob.Name)
		return true
	}
	if strings.HasSuffix(*blob.Name, "/") {
		d.log.V(1).Info("ignore downloading azure object with trailing '/'", "azure_path", blob.Name)
		return true
	}
	// If the path does not end with a slash, make sure we only get exact path matches. For example,
	// with the prefix "models/mnist", we don't want to match path "models/mnist2/model.zip".
	if !strings.HasSuffix(prefix, "/") && !strings.HasPrefix(*blob.Name, prefix+"/") && !(*blob.Name == prefix) {
		d.log.V(1).Info("ignore downloading azure object with prefix not followed by a slash", "prefix", prefix, "azure_path", blob.Name)
		return true
	}
	return false
}
