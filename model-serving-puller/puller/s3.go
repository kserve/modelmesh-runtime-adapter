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
package puller

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/go-logr/logr"

	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials"
	"github.com/IBM/ibm-cos-sdk-go/aws/session"
	"github.com/IBM/ibm-cos-sdk-go/service/s3"
	"github.com/IBM/ibm-cos-sdk-go/service/s3/s3manager"
	"github.com/kserve/modelmesh-runtime-adapter/internal/modelschema"
	"github.com/kserve/modelmesh-runtime-adapter/internal/util"
)

type storage struct {
	storagekey string
	bucketname string
	path       string
}

// S3Downloader interface to s3 functions. This is a wrapper around the aws sdk and is
// useful for testing because we can easily mock out this interface.
type S3Downloader interface {
	ListObjectsUnderPrefix(bucket string, prefix string) ([]string, error)
	DownloadWithIterator(ctx aws.Context, iter s3manager.BatchDownloadIterator, opts ...func(*s3manager.Downloader)) error
	IsSameConfig(config interface{}) bool
}

// s3Downloader implements the S3Downloader interface
type s3Downloader struct {
	downloader *s3manager.Downloader
	client     *s3.S3
	Log        logr.Logger
	config     *StorageConfiguration
}

func (s *Puller) getS3Downloader(storageKey string, config *StorageConfiguration) (S3Downloader, error) {
	s.mutex.RLock()
	downloader, ok := s.s3DownloaderCache[storageKey]
	s.mutex.RUnlock()
	if ok {
		if downloader.IsSameConfig(config) {
			return downloader, nil
		}
	}

	// make sure the config and cache have not changed
	s.mutex.Lock()
	defer s.mutex.Unlock()
	updatedStorageConfig, err := s.PullerConfig.GetStorageConfiguration(storageKey, s.Log)
	if err != nil {
		return nil, err
	}

	updatedDownloader, ok := s.s3DownloaderCache[storageKey]
	if ok {
		if updatedDownloader.IsSameConfig(updatedStorageConfig) {
			return updatedDownloader, nil
		}
	}
	newDownloader, err := s.NewS3DownloaderFromConfig(updatedStorageConfig, s.PullerConfig.S3DownloadConcurrency, s.Log)
	if err != nil {
		delete(s.s3DownloaderCache, storageKey)
		return nil, err
	}

	s.s3DownloaderCache[storageKey] = newDownloader

	return newDownloader, nil
}

// NewS3Downloader creates a new client connection to the s3 server and returns it as an S3Downloader
func NewS3Downloader(config *StorageConfiguration, downloadConcurrency int, log logr.Logger) (S3Downloader, error) {
	// Create a connection to the S3 server

	conf := aws.NewConfig().
		WithS3ForcePathStyle(true).
		WithEndpoint(config.EndpointURL).
		WithRegion(config.Region)
	conf = conf.WithCredentials(credentials.NewStaticCredentialsFromCreds(credentials.Value{
		AccessKeyID: config.AccessKeyID, SecretAccessKey: config.SecretAccessKey,
	}))

	sess := session.Must(session.NewSession())
	if len(config.Certificate) > 0 {
		sess = session.Must(session.NewSessionWithOptions(session.Options{
			CustomCABundle: strings.NewReader(config.Certificate),
		}))
	}

	s3Client := s3.New(sess, conf)
	downloader := s3manager.NewDownloaderWithClient(s3Client, func(d *s3manager.Downloader) {
		d.Concurrency = downloadConcurrency
	})
	// TODO does this ping the s3 to validate credentials?
	// TODO do we need to defer closing the s3Client?

	return &s3Downloader{downloader: downloader, client: s3Client, config: config, Log: log}, nil
}

func (s *Puller) DownloadFromCOS(modelID string, objPath string, schemaPath string, storageKey string, storageConfig *StorageConfiguration, s3Params map[string]interface{}) (string, error) {
	bucketName, ok := s3Params[jsonAttrModelKeyBucket].(string)
	if !ok {
		if s3Params[jsonAttrModelKeyBucket] != nil {
			return "", fmt.Errorf("Invalid storageParams in LoadModelRequest, '%s' attribute must have a string value. Found value %v", jsonAttrModelKeyBucket, s3Params[jsonAttrModelKeyBucket])
		}
		// no bucket attribute specified, fall down to the default
		bucketName = ""
	}

	if bucketName == "" {
		bucketName = storageConfig.DefaultBucket
	}

	if bucketName == "" {
		return "", fmt.Errorf("Storage bucket was not specified in the LoadModel request and there is no default bucket in the storage configuration")
	}

	downloader, err := s.getS3Downloader(storageKey, storageConfig)
	if err != nil {
		return "", err
	}

	objectsToDownload, err := downloader.ListObjectsUnderPrefix(bucketName, objPath)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() != s3.ErrCodeNoSuchBucket {
				s.Log.Info("Deleting downloader from cache", "storage_key", storageKey)
				s.mutex.Lock()
				delete(s.s3DownloaderCache, storageKey)
				s.mutex.Unlock()
			}
		}
		return "", fmt.Errorf("Unable to list items in bucket '%s', %s", bucketName, err)
	}

	if len(objectsToDownload) == 0 {
		return "", fmt.Errorf("no objects found for path '%s'", objPath)
	}

	// Skip explicitly downloading SchemaFile, if already exists in model objects to be downloaded
	schemaInModelPath := ModelObjectsContainsSchema(objectsToDownload, schemaPath)
	if schemaPath != "" && !schemaInModelPath {

		schemaToDownload, serr := downloader.ListObjectsUnderPrefix(bucketName, schemaPath)
		if serr != nil {
			if aerr, ok := err.(awserr.Error); ok {
				if aerr.Code() != s3.ErrCodeNoSuchBucket {
					s.Log.Info("Deleting downloader from cache", "storage_key", storageKey)
					s.mutex.Lock()
					delete(s.s3DownloaderCache, storageKey)
					s.mutex.Unlock()
				}
			}
			return "", fmt.Errorf("Unable to list items in bucket '%s', %s", bucketName, serr)
		}

		if len(schemaToDownload) == 1 {
			schemaStorage := storage{storagekey: storageKey, bucketname: bucketName, path: schemaPath}
			p, serr := s.DownloadObjectsfromCOS(modelID, &schemaStorage, schemaToDownload, downloader)

			if serr != nil {
				return "", fmt.Errorf("Error downloading schema from path '%s'", schemaPath)
			}

			if filepath.Base(schemaPath) != modelschema.ModelSchemaFile {
				s, serr := util.SecureJoin(s.PullerConfig.RootModelDir, modelID, modelschema.ModelSchemaFile)
				if serr != nil {
					return "", serr
				}
				serr = os.Rename(p, s)
				if serr != nil {
					return "", serr
				}
			}

		}

		// Report error if more than one file provided in schema path
		if len(schemaToDownload) > 1 {
			return "", fmt.Errorf("Expected one file in schema path '%s', more than one provided", schemaPath)
		}
	}

	modelStorage := storage{storagekey: storageKey, bucketname: bucketName, path: objPath}
	p, err := s.DownloadObjectsfromCOS(modelID, &modelStorage, objectsToDownload, downloader)

	if err != nil {
		return "", fmt.Errorf("Error downloading model from path '%s':%w", objPath, err)
	}

	// If schema found in model path and not _schema.json, rename it
	if schemaInModelPath && filepath.Base(schemaPath) != modelschema.ModelSchemaFile {
		sp, serr := util.SecureJoin(s.PullerConfig.RootModelDir, modelID, filepath.Base(schemaPath))
		if serr != nil {
			return "", serr
		}

		tp, serr := util.SecureJoin(s.PullerConfig.RootModelDir, modelID, modelschema.ModelSchemaFile)
		if serr != nil {
			return "", serr
		}

		serr = os.Rename(sp, tp)
		if serr != nil {
			return "", serr
		}
	}

	return p, nil
}

func ModelObjectsContainsSchema(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}

func (s *Puller) DownloadObjectsfromCOS(modelID string, storage *storage, objectsToDownload []string, downloader S3Downloader) (string, error) {

	objPath := storage.path

	userSpecifiedFile := ""

	downloadIter := s3manager.DownloadObjectsIterator{}
	log := s.Log.WithValues("bucket", storage.bucketname)

	for _, object := range objectsToDownload {
		relativePath := strings.TrimPrefix(object, objPath)
		if relativePath == "" {
			// handle single file case
			relativePath = filepath.Base(object)
			userSpecifiedFile = relativePath
		}
		filePath, joinErr := util.SecureJoin(s.PullerConfig.RootModelDir, modelID, relativePath)
		if joinErr != nil {
			log.Error(joinErr, "Error joining paths", "RootModelDir", s.PullerConfig.RootModelDir, "modelID", modelID, "relativePath", relativePath)
			return "", joinErr
		}
		file, fileErr := openFile(filePath, log)
		if fileErr != nil {
			return "", fmt.Errorf("Unable to download items in bucket '%s', %s", storage.bucketname, fileErr)
		}
		defer file.Close()
		log.V(1).Info("File to download", "s3_path", object, "destination", file.Name())

		downloadIter.Objects = append(downloadIter.Objects, s3manager.BatchDownloadObject{
			Object: &s3.GetObjectInput{
				Bucket: aws.String(storage.bucketname),
				Key:    aws.String(object),
			},
			Writer: file,
		})
	}

	log.Info("Downloading files from s3 storage", "path", storage.path, "number_of_files", len(downloadIter.Objects))
	downloadErr := downloader.DownloadWithIterator(aws.BackgroundContext(), &downloadIter)
	if downloadErr != nil {
		s.mutex.Lock()
		delete(s.s3DownloaderCache, storage.storagekey)
		s.mutex.Unlock()
		return "", fmt.Errorf("Unable to download items in bucket '%s', %s", storage.bucketname, downloadErr)
	}

	p, err := util.SecureJoin(s.PullerConfig.RootModelDir, modelID, userSpecifiedFile)
	if err != nil {
		return "", err
	}

	return p, nil
}

func (s *s3Downloader) ListObjectsUnderPrefix(bucket string, prefix string) ([]string, error) {
	objectPaths := make([]string, 0, 10)
	log := s.Log.WithValues("bucket", bucket)
	log.V(1).Info("Listing objects from s3", "prefix", prefix)
	err := s.client.ListObjectsV2Pages(&s3.ListObjectsV2Input{
		Bucket:  aws.String(bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int64(100), // list 100 at a time
	}, func(listObjectsResult *s3.ListObjectsV2Output, lastPage bool) bool {
		// turn the results into a string array, ignoring 0 byte objects and objects ending with a '/'
		for _, object := range listObjectsResult.Contents {
			if *object.Size == 0 {
				log.V(1).Info("Ignore downloading s3 object of 0 byte size", "s3_path", *object.Key)
				continue
			}
			if strings.HasSuffix(*object.Key, "/") {
				log.V(1).Info("Ignore downloading s3 object with trailing '/'", "s3_path", *object.Key)
				continue
			}
			// If the path does not end with a slash, make sure we only get exact path matches. For example,
			// with the prefix "models/mnist", we don't want to match path "models/mnist2/model.zip".
			if !strings.HasSuffix(prefix, "/") && !strings.HasPrefix(*object.Key, prefix+"/") && !(*object.Key == prefix) {
				log.V(1).Info("Ignore downloading s3 object matching part of the path", "prefix", prefix, "s3_path", *object.Key)
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

func (s *s3Downloader) DownloadWithIterator(ctx aws.Context, iter s3manager.BatchDownloadIterator, opts ...func(*s3manager.Downloader)) error {
	return s.downloader.DownloadWithIterator(ctx, iter, opts...)
}

func openFile(path string, log logr.Logger) (*os.File, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("Invalid argument, require absolute path: %s", path)
	}

	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		// error path is an existing dir
		return nil, fmt.Errorf("s3 file already exists as a directory '%s' in the local file system. "+
			"This most likely means the model is setup incorrectly in object storage.", path)
	} else if err != nil && os.IsNotExist(err) {
		// the path does not exist, let's create the parent dirs
		fileDir := filepath.Dir(path)
		log.V(2).Info("Creating directories", "local_dir", path)
		mkdirErr := os.MkdirAll(fileDir, os.ModePerm)
		if mkdirErr != nil {
			// problem making the parent dirs, maybe somewhere in the path was an existing file
			return nil, fmt.Errorf("error creating directory structure '%s' in the local file system. "+
				"This most likely means the model is setup incorrectly in object storage. Error details: %v", fileDir, mkdirErr)
		}
	} else if err != nil {
		return nil, fmt.Errorf("error trying to stat local file '%s'. "+
			"This most likely means the model is setup incorrectly in object storage. Error details: %v", path, err)
	}

	log.V(2).Info("Opening file", "file_path", path)
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("OS error creating file '%s' %v", path, err)
	}
	return file, nil
}

func (s *s3Downloader) IsSameConfig(config interface{}) bool {
	return reflect.DeepEqual(config, s.config)
}
