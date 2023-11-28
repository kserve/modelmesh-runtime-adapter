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

package s3provider

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
	configAccessKeyID     = "access_key_id"
	configSecretAccessKey = "secret_access_key"
	configEndpoint        = "endpoint_url"
	configRegion          = "region"
	configBucket          = "bucket"
	configCertificate     = "certificate"
)

// interfaces

// s3DownloaderFactory is the interface used create s3 downloaders
// useful to mock for testing
type s3DownloaderFactory interface {
	newDownloader(log logr.Logger, accessKeyID, secretAccessKey, endpoint, region, certificate string) s3Downloader
}

// s3Downloader is the interface used to download resources from s3
// useful to mock for testing
type s3Downloader interface {
	listObjects(bucket, prefix string) ([]string, error)
	downloadBatch(ctx context.Context, bucket string, targets []pullman.Target) error
}

// structs

type s3Provider struct {
	s3DownloaderFactory s3DownloaderFactory
}

// s3Provider implements StorageProvider
var _ pullman.StorageProvider = (*s3Provider)(nil)

// Only changes to the bucket can be handled by a single client
func (p s3Provider) GetKey(config pullman.Config) string {
	// hash the values of all configurations that go into creating the client
	// no need to validate the config here
	accessKeyID, _ := pullman.GetString(config, configAccessKeyID)
	secretAccessKey, _ := pullman.GetString(config, configSecretAccessKey)
	endpoint, _ := pullman.GetString(config, configEndpoint)
	region, _ := pullman.GetString(config, configRegion)
	certificate, _ := pullman.GetString(config, configCertificate)

	return pullman.HashStrings(endpoint, region, accessKeyID, secretAccessKey, certificate)
}

func (p s3Provider) NewRepository(config pullman.Config, log logr.Logger) (pullman.RepositoryClient, error) {
	missingRequiredStringConfigTemplate := "missing required string configuration '%s'"

	accessKeyID, ok := pullman.GetString(config, configAccessKeyID)
	if !ok {
		return nil, fmt.Errorf(missingRequiredStringConfigTemplate, configAccessKeyID)
	}

	secretAccessKey, ok := pullman.GetString(config, configSecretAccessKey)
	if !ok {
		return nil, fmt.Errorf(missingRequiredStringConfigTemplate, configSecretAccessKey)
	}

	endpoint, ok := pullman.GetString(config, configEndpoint)
	if !ok {
		return nil, fmt.Errorf(missingRequiredStringConfigTemplate, configEndpoint)
	}

	region, ok := pullman.GetString(config, configRegion)
	if !ok {
		return nil, fmt.Errorf(missingRequiredStringConfigTemplate, configRegion)
	}

	// certificate is optional
	certificate, _ := pullman.GetString(config, configCertificate)

	return &s3RepositoryClient{
		s3client: p.s3DownloaderFactory.newDownloader(log, accessKeyID, secretAccessKey, endpoint, region, certificate),
		log:      log,
	}, nil

}

type s3RepositoryClient struct {
	s3client s3Downloader
	log      logr.Logger
}

// s3RepositoryClient implements RepositoryClient
var _ pullman.RepositoryClient = (*s3RepositoryClient)(nil)

func (r *s3RepositoryClient) Pull(ctx context.Context, pc pullman.PullCommand) error {
	destDir := pc.Directory
	targets := pc.Targets

	// process per-command configuration
	bucket, ok := pullman.GetString(pc.RepositoryConfig, configBucket)
	if !ok {
		// we need a value for bucket
		return errors.New("required configuration 'bucket' missing from command")
	}

	// resolve full paths of objects to download and local paths for the resulting files
	//  mainly, this means resolving the objects referenced by a "directory" in s3
	resolvedTargets := make([]pullman.Target, 0, len(targets))
	for _, pt := range targets {
		objPaths, err := r.s3client.listObjects(bucket, pt.RemotePath)
		if err != nil {
			return fmt.Errorf("unable to list objects in bucket '%s': %w", bucket, err)
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

	downloadErr := r.s3client.downloadBatch(ctx, bucket, resolvedTargets)
	if downloadErr != nil {
		return fmt.Errorf("unable to download objects in bucket '%s': %w", bucket, downloadErr)
	}

	return nil
}

func init() {
	p := s3Provider{
		s3DownloaderFactory: ibmS3DownloaderFactory{
			downloaderConcurrency: 10,
		},
	}
	pullman.RegisterProvider("s3", p)
}
