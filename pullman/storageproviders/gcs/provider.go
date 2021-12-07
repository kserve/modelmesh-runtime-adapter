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
package gcsprovider

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
	"github.com/kserve/modelmesh-runtime-adapter/pullman"
)

const (
	configBucket = "bucket"
)

// gcsDownloaderFactory is the interface used create GCS downloaders
// useful to mock for testing
type gcsDownloaderFactory interface {
	newDownloader(log logr.Logger) (gcsDownloader, error)
}

// gcsDownloader is the interface used to download resources from GCS
// useful to mock for testing
type gcsDownloader interface {
	listObjects(ctx context.Context, bucket string, prefix string) ([]string, error)
	downloadBatch(ctx context.Context, bucket string, targets []pullman.Target) error
}

type gcsProvider struct {
	gcsDownloaderFactory gcsDownloaderFactory
}

// gcsProvider implements StorageProvider
var _ pullman.StorageProvider = (*gcsProvider)(nil)

func (p gcsProvider) GetKey(config pullman.Config) string {
	b, _ := json.Marshal(config)
	hsha1 := sha1.Sum(b)
	return hex.EncodeToString(hsha1[:5])
}

func (p gcsProvider) NewRepository(config pullman.Config, log logr.Logger) (pullman.RepositoryClient, error) {
	cl, err := p.gcsDownloaderFactory.newDownloader(log)
	if err != nil {
		return nil, err
	}
	return &gcsRepositoryClient{
		gcsclient: cl,
		log:       log,
	}, nil
}

type gcsRepositoryClient struct {
	gcsclient gcsDownloader
	log       logr.Logger
}

// gcsRepository implements RepositoryClient
var _ pullman.RepositoryClient = (*gcsRepositoryClient)(nil)

func (r *gcsRepositoryClient) Pull(ctx context.Context, pc pullman.PullCommand) error {
	destDir := pc.Directory
	targets := pc.Targets

	// Process per-command configuration
	bucket, ok := pullman.GetString(pc.RepositoryConfig, configBucket)
	if !ok {
		return errors.New("required configuration 'bucket' missing from command")
	}

	// Resolve full paths of objects to download and local paths for the resulting files
	// Mainly, this means resolving the objects referenced by a "directory" in GCS
	resolvedTargets := make([]pullman.Target, 0, len(targets))
	for _, pt := range targets {
		objPaths, err := r.gcsclient.listObjects(ctx, bucket, pt.RemotePath)

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

	downloadErr := r.gcsclient.downloadBatch(ctx, bucket, resolvedTargets)
	if downloadErr != nil {
		return fmt.Errorf("unable to download objects in bucket '%s': %w", bucket, downloadErr)
	}

	return nil

}

func init() {
	p := gcsProvider{
		gcsDownloaderFactory: gcsClientFactory{},
	}
	pullman.RegisterProvider("gs", p)
}
