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

package pullman

import (
	"context"

	"github.com/go-logr/logr"
)

// A StorageProvider is a factory for RepositoryClients
type StorageProvider interface {
	NewRepository(config Config, log logr.Logger) (RepositoryClient, error)
	// GetKey generates a string from the config that only includes fields
	// required to build the connection to the storage service. If the key
	// of two configs match, a single RepositoryClient must be able to
	// handle pulls with both configs.
	// Note: GetKey should not validate the config
	GetKey(config Config) string
}

// A RepositoryClient is the worker that executes a PullCommand
type RepositoryClient interface {
	Pull(context.Context, PullCommand) error
}

// Represents the command sent to PullMan to be fulfilled
type PullCommand struct {
	// repository from which files will be pulled
	RepositoryConfig Config
	// local directory where files will be pulled to
	Directory string
	// the list of paths referring to resources to be pulled
	Targets []Target
}

type Target struct {
	// remote path to resource(s) to be pulled
	RemotePath string
	// filepath to write the file(s) to
	LocalPath string
}
