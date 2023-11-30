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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type placeholderClient struct{}

func (*placeholderClient) Pull(context.Context, PullCommand) error {
	return nil
}

func Test_Cache_SetAndGet(t *testing.T) {
	cache := newClientCache(zap.New(), 0, 0)

	// set a single client
	client1 := &placeholderClient{}
	key1 := "key1"
	cache.setClient(key1, client1)

	// assert we can get that client
	{
		c, ok := cache.getClient(key1)
		assert.True(t, ok, "expected to be able to set then get a client")
		assert.Same(t, client1, c)
	}

	assert.Equal(t, 1, len(cache.cache))

	// set a second client
	client2 := &placeholderClient{}
	key2 := "key2"
	cache.setClient(key2, client2)

	// assert we can get each client
	{
		got, ok := cache.getClient(key1)
		assert.True(t, ok, "expected to be able to set then get two clients")
		assert.Same(t, client1, got)
	}
	{
		got, ok := cache.getClient(key2)
		assert.True(t, ok, "expected to be able to set then get two clients")
		assert.Same(t, client2, got)
	}

	assert.Equal(t, 2, len(cache.cache))
}

func Test_Cache_ClientLifecycle(t *testing.T) {
	cache := newClientCache(zap.New(), 1*time.Second, 0) // need non-zero TTL to allow expiring

	client := &placeholderClient{}
	key := "some key"
	cache.setClient(key, client)
	// should have one entry in cache
	assert.Equal(t, 1, len(cache.cache))

	// set the expiration to the past
	// then call getClient which should refresh the TTL
	// then run the cleanup
	cache.cache[key].expiry = time.Now().Add(-time.Hour)
	_, _ = cache.getClient(key)
	cache.runCleanup()

	// the client should not have been removed
	{
		_, ok := cache.getClient(key)
		assert.True(t, ok, "expected the client to not expire")
		assert.Equal(t, 1, len(cache.cache))
	}

	// set the expiration to the past
	// but don't call getClient
	// then run the cleanup
	cache.cache[key].expiry = time.Now().Add(-time.Hour)
	cache.runCleanup()

	// now there should be no entries
	_, ok := cache.getClient(key)
	assert.False(t, ok, "expected the cache entry to expire")
	assert.Equal(t, 0, len(cache.cache))
}
