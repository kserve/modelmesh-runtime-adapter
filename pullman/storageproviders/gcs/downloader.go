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
package gcsprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"cloud.google.com/go/storage"
	"github.com/go-logr/logr"
	"github.com/kserve/modelmesh-runtime-adapter/pullman"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

type gcsClientFactory struct{}

// gcsClientFactory implements gcsDownloaderFactory
var _ gcsDownloaderFactory = (*gcsClientFactory)(nil)

// gcsImplDownloader implements gcsDownloader
var _ gcsDownloader = (*gcsImplDownloader)(nil)

func (f gcsClientFactory) newDownloader(log logr.Logger, credentials map[string]string) (gcsDownloader, error) {
	ctx := context.Background()
	var cl *storage.Client
	var err error

	if len(credentials) > 0 {
		credJson, marshalErr := json.Marshal(credentials)
		if marshalErr != nil {
			return nil, fmt.Errorf("unable to marshal json credentials: %w", marshalErr)
		}
		cl, err = storage.NewClient(ctx, option.WithCredentialsJSON(credJson))
	} else {
		cl, err = storage.NewClient(ctx, option.WithoutAuthentication())
	}

	if err != nil {
		return nil, err
	}

	return &gcsImplDownloader{
		client: cl,
		log:    log,
	}, nil
}

type gcsImplDownloader struct {
	client *storage.Client
	log    logr.Logger

	wg  sync.WaitGroup
	mu  sync.Mutex
	err error
}

func (d *gcsImplDownloader) listObjects(ctx context.Context, bucket string, prefix string) ([]string, error) {
	query := &storage.Query{Prefix: prefix}
	it := d.client.Bucket(bucket).Objects(ctx, query)

	objectPaths := make([]string, 0, 10)

	for {
		obj, err := it.Next()
		if err == iterator.Done {
			return objectPaths, nil
		}
		if err != nil {
			return nil, fmt.Errorf("GCS listObjects: unable to list bucket %q: %v", bucket, err)
		}

		if !d.shouldIgnoreObject(obj, prefix) {
			objectPaths = append(objectPaths, obj.Name)
		}
	}
}

func (d *gcsImplDownloader) downloadBatch(ctx context.Context, bucket string, targets []pullman.Target) error {
	ch := make(chan pullman.Target)

	workerCount := maxDownloadConcurrency
	if len(targets) < workerCount {
		workerCount = len(targets)
	}

	d.wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer d.wg.Done()
			if err := d.downloadFilesFromChannel(ctx, bucket, ch); err != nil {
				d.setError(err)
			}
		}()
	}

	for _, target := range targets {
		ch <- target
	}

	close(ch)
	d.wg.Wait()

	return d.err
}

func (d *gcsImplDownloader) downloadFilesFromChannel(ctx context.Context, bucket string, ch <-chan pullman.Target) error {
	for {
		target, ok := <-ch
		if !ok {
			break
		}

		if d.getError() != nil {
			// Empty the rest of the channel if there is an error.
			continue
		}

		file, err := pullman.OpenFile(target.LocalPath)
		if err != nil {
			return fmt.Errorf("unable to open local file '%s' for writing: %w", target.LocalPath, err)
		}
		defer file.Close()
		d.log.V(1).Info("downloading object", "path", target.RemotePath, "filename", target.LocalPath)
		reader, err := d.client.Bucket(bucket).Object(target.RemotePath).NewReader(ctx)
		if err != nil {
			return fmt.Errorf("failed to create reader for object(%s) in bucket(%s): %v", target.RemotePath, bucket, err)
		}
		defer reader.Close()
		if _, err = io.Copy(file, reader); err != nil {
			return fmt.Errorf("failed to write data to file(%s): from object(%s) in bucket(%s): %v",
				file.Name(), target.RemotePath, bucket, err)
		}
	}
	return nil
}

func (d *gcsImplDownloader) getError() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

func (d *gcsImplDownloader) setError(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.err = err
}

func (d *gcsImplDownloader) shouldIgnoreObject(object *storage.ObjectAttrs, prefix string) bool {
	if object.Size == 0 {
		d.log.V(1).Info("ignore downloading gcs object of 0 byte size", "gcs_path", object.Name)
		return true
	}
	if strings.HasSuffix(object.Name, "/") {
		d.log.V(1).Info("ignore downloading gcs object with trailing '/'", "gcs_path", object.Name)
		return true
	}
	// If the path does not end with a slash, make sure we only get exact path matches. For example,
	// with the prefix "models/mnist", we don't want to match path "models/mnist2/model.zip".
	if !strings.HasSuffix(prefix, "/") && !strings.HasPrefix(object.Name, prefix+"/") && !(object.Name == prefix) {
		d.log.V(1).Info("ignore downloading gcs object with prefix not followed by a slash", "prefix", prefix, "gcs_path", object.Name)
		return true
	}
	return false
}
