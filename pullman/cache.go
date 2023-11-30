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
	"sync"
	"time"

	"github.com/go-logr/logr"
)

type cacheEntry struct {
	key    string
	client RepositoryClient
	ttl    time.Duration
	expiry time.Time
}

func (ce *cacheEntry) resetExpiry() {
	if ce.ttl > 0 {
		ce.expiry = time.Now().Add(ce.ttl)
	}
}

func (ce *cacheEntry) isExpired() bool {
	if ce.ttl <= 0 {
		return false
	}
	return ce.expiry.Before(time.Now())
}

// clientCache is used to track instances of repository clients including
// limiting the number of clients that can be managed at a time
type clientCache struct {
	cache map[string]*cacheEntry
	ttl   time.Duration
	lock  sync.Mutex
	log   logr.Logger
}

func newClientCache(log logr.Logger, ttl time.Duration, cleanupPeriod time.Duration) *clientCache {
	cc := clientCache{
		cache: map[string]*cacheEntry{},
		ttl:   ttl,
		log:   log.WithName("client-cache"),
	}

	cc.startBackgroundExpiryProcess(cleanupPeriod)

	return &cc
}

func (cc *clientCache) getClient(key string) (RepositoryClient, bool) {
	cc.lock.Lock()
	defer cc.lock.Unlock()

	ce, ok := cc.getEntry(key)
	if !ok {
		return nil, false
	}
	ce.resetExpiry()
	return ce.client, true
}

func (cc *clientCache) setClient(key string, client RepositoryClient) {
	cc.lock.Lock()
	defer cc.lock.Unlock()

	if existingEntry, exists := cc.getEntry(key); exists {
		existingEntry.client = client
		existingEntry.resetExpiry()
		return
	}

	newEntry := cacheEntry{
		key:    key,
		client: client,
		ttl:    cc.ttl,
	}
	newEntry.resetExpiry()

	cc.cache[key] = &newEntry
}

// internals

// getEntry assumes the mutex is locked
func (cc *clientCache) getEntry(key string) (*cacheEntry, bool) {
	ce, ok := cc.cache[key]
	return ce, ok
}

func (cc *clientCache) startBackgroundExpiryProcess(period time.Duration) {
	// NewTicker panics if passed a non-positive duration
	// this also gives a way to disable the cache cleaning
	if period <= 0 {
		return
	}

	ticker := time.NewTicker(period)
	go func() {
		cc.log.Info("starting clean up of cached clients")
		for range ticker.C {
			cc.runCleanup()
		}
	}()
}

func (cc *clientCache) runCleanup() {
	// we don't want to lock the mutex unless we may need to
	// expire an entry, so build a list of entries to check
	// first without locking
	entriesToCheck := make([]*cacheEntry, 0, 3) // pre-allocate capacity for a few entries
	for _, ce := range cc.cache {
		if ce.isExpired() {
			entriesToCheck = append(entriesToCheck, ce)
		}
	}

	if len(entriesToCheck) == 0 {
		cc.log.Info("no cached clients are candidates for expiration")
		return
	}

	// now lock the mutex as we do the final check
	cc.lock.Lock()
	for _, ce := range entriesToCheck {
		// check the expiry again in the lock
		if ce.isExpired() {
			cc.log.Info("expiring cached client", "key", ce.key)
			delete(cc.cache, ce.key)
		}
	}
	cc.lock.Unlock()
}
