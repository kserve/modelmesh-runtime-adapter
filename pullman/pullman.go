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
package pullman

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

// package global map to track registered storage providers
var registeredStorageProviders map[string]StorageProvider

const (
	defaultCacheTTL    = 1 * time.Hour
	defaultCachePeriod = 4 * time.Hour
)

type PullManager struct {
	lock sync.Mutex
	log  logr.Logger

	// catalog of registered storage providers
	storageProviders map[string]StorageProvider
	// cache of repository clients
	clientCache *clientCache
}

func NewPullManager(log logr.Logger, options ...func(*PullManager)) *PullManager {
	pm := &PullManager{
		lock:             sync.Mutex{},
		log:              log,
		storageProviders: registeredStorageProviders,
	}

	for _, option := range options {
		option(pm)
	}
	// default for the client cache
	if pm.clientCache == nil {
		pm.clientCache = newClientCache(log, defaultCacheTTL, defaultCachePeriod)
	}

	return pm
}

func CacheOptions(ttl time.Duration, cleanupPeriod time.Duration) func(*PullManager) error {
	return func(pm *PullManager) error {
		pm.clientCache = newClientCache(pm.log, ttl, cleanupPeriod)
		return nil
	}
}

// Pull processes the PullCommand, pulling files to the local filesystem
func (p *PullManager) Pull(ctx context.Context, pc PullCommand) error {
	// TODO: common functionality across pullers
	//  checks on existing files in the download directory?
	//  check/resolve name conflicts in the requested resources?
	//  manage the number of concurrent pulls?

	repo, err := p.getRepositoryClient(pc.RepositoryConfig)
	if err != nil {
		return fmt.Errorf("could not process pull command: %w", err)
	}

	return repo.Pull(ctx, pc)
}

func (p *PullManager) getRepositoryClient(config Config) (RepositoryClient, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	// get a key from the config
	repoType := config.GetType()
	provider, providerFound := p.storageProviders[repoType]
	if !providerFound {
		return nil, fmt.Errorf("no provider registered for repositories of type '%s'", repoType)
	}
	k := provider.GetKey(config)
	cacheKey := fmt.Sprintf("%s|%s", repoType, k)
	log := p.log.WithValues("type", repoType, "cacheKey", cacheKey)

	// check if we have a compatible client in the cache
	if rc, found := p.clientCache.getClient(cacheKey); found {
		log.V(1).Info("found existing client in cache")
		return rc, nil
	}

	log.V(1).Info("creating new repository client")
	newClient, err := provider.NewRepository(config, log)
	if err != nil {
		return nil, fmt.Errorf("unable to create repository of type '%s': %w", repoType, err)
	}
	// cache the repo
	p.clientCache.setClient(cacheKey, newClient)

	return newClient, nil
}

// RegisterProvider should only be called when initializing the application
func RegisterProvider(providerType string, provider StorageProvider) {
	if _, ok := registeredStorageProviders[providerType]; ok {
		panic(fmt.Sprintf("cannot register a second StorageProvider of type '%s'", providerType))
	}
	registeredStorageProviders[providerType] = provider
}

func init() {
	registeredStorageProviders = make(map[string]StorageProvider)
}
