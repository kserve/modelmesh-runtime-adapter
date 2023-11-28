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
	"fmt"
	"strings"

	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials"
	"github.com/IBM/ibm-cos-sdk-go/aws/session"
	"github.com/IBM/ibm-cos-sdk-go/service/s3"
	"github.com/IBM/ibm-cos-sdk-go/service/s3/s3manager"
	"github.com/go-logr/logr"

	"github.com/kserve/modelmesh-runtime-adapter/pullman"
)

type ibmS3DownloaderFactory struct {
	downloaderConcurrency int
}

// ibmS3DownloaderFactory implements s3DownloaderFactory
var _ s3DownloaderFactory = (*ibmS3DownloaderFactory)(nil)

func (f ibmS3DownloaderFactory) newDownloader(log logr.Logger, accessKeyID, secretAccessKey, endpoint, region, certificate string) s3Downloader {
	s3Config := aws.NewConfig().
		WithS3ForcePathStyle(true).
		WithEndpoint(endpoint).
		WithRegion(region).
		WithCredentials(credentials.NewStaticCredentialsFromCreds(credentials.Value{
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
		}))

	var s3Session *session.Session
	if certificate != "" {
		s3Session = session.Must(session.NewSessionWithOptions(session.Options{
			CustomCABundle: strings.NewReader(certificate),
		}))
	} else {
		s3Session = session.Must(session.NewSession())
	}

	return &ibmS3Downloader{
		client:                s3.New(s3Session, s3Config),
		log:                   log,
		downloaderConcurrency: f.downloaderConcurrency,
	}
}

type ibmS3Downloader struct {
	client                *s3.S3
	log                   logr.Logger
	downloaderConcurrency int
}

// ibmS3Downloader implements s3Downloader
var _ s3Downloader = (*ibmS3Downloader)(nil)

func (d *ibmS3Downloader) listObjects(bucket string, prefix string) ([]string, error) {
	objectPaths := make([]string, 0, 10)
	err := d.client.ListObjectsV2Pages(&s3.ListObjectsV2Input{
		Bucket:  aws.String(bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int64(100),
	}, func(listObjectsResult *s3.ListObjectsV2Output, lastPage bool) bool {
		// turn the results into a string array, ignoring 0 byte objects and objects ending with a '/'
		for _, object := range listObjectsResult.Contents {
			if d.shouldIgnoreObject(object, prefix) {
				continue
			}
			objectPaths = append(objectPaths, *object.Key)
		}
		return lastPage // continue until the last page
	})
	if err != nil {
		return nil, err
	}
	return objectPaths, nil
}

// downloadBatch
// assumes that `targets` has a separate entry for each object to download and
// LocalPath is the full path to the desired target file
func (d *ibmS3Downloader) downloadBatch(ctx context.Context, bucket string, targets []pullman.Target) error {
	downloadIter := s3manager.DownloadObjectsIterator{}
	for _, target := range targets {
		file, fileErr := pullman.OpenFile(target.LocalPath)
		if fileErr != nil {
			return fmt.Errorf("unable to open local file '%s' for writing: %w", target.LocalPath, fileErr)
		}
		defer file.Close()

		d.log.V(1).Info("downloading object", "path", target.RemotePath, "filename", target.LocalPath)
		downloadIter.Objects = append(downloadIter.Objects, s3manager.BatchDownloadObject{
			Object: &s3.GetObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(target.RemotePath),
			},
			Writer: file,
		})
	}

	if len(downloadIter.Objects) == 0 {
		d.log.Info("no objects to download")
		return nil
	}

	downloader := s3manager.NewDownloaderWithClient(d.client, func(s3d *s3manager.Downloader) {
		s3d.Concurrency = d.downloaderConcurrency
	})
	downloadErr := downloader.DownloadWithIterator(ctx, &downloadIter)
	if downloadErr != nil {
		return fmt.Errorf("unable to download objects in bucket '%s': %w", bucket, downloadErr)
	}

	return nil
}

func (d *ibmS3Downloader) shouldIgnoreObject(object *s3.Object, prefix string) bool {
	if *object.Size == 0 {
		d.log.V(1).Info("ignore downloading s3 object of 0 byte size", "s3_path", *object.Key)
		return true
	}
	if strings.HasSuffix(*object.Key, "/") {
		d.log.V(1).Info("ignore downloading s3 object with trailing '/'", "s3_path", *object.Key)
		return true
	}
	// If the path does not end with a slash, make sure we only get exact path matches. For example,
	// with the prefix "models/mnist", we don't want to match path "models/mnist2/model.zip".
	if !strings.HasSuffix(prefix, "/") && !strings.HasPrefix(*object.Key, prefix+"/") && !(*object.Key == prefix) {
		d.log.V(1).Info("ignore downloading s3 object with prefix not followed by a slash", "prefix", prefix, "s3_path", *object.Key)
		return true
	}
	return false
}
