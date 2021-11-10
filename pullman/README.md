# PullMan

Manage your file pulls with PullMan. The primary use-case is supporting a
long-running process that can have remote repositories configured dynamically
and concurrent pulls from those repositories handled efficiently.

This project is a work in progress.

## API

There is a single method in the functional API:

```go
func (p *PullManager) Pull(ctx context.Context, pc PullCommand) error
```

The `PullCommand` contains all the needed information to process a request to
pull resources. A `PullManager` instance is intended to be used concurrently
from multiple threads calling `Pull()`.

See [Concepts](#concepts) for details.

### Example Usage

```go
package main

import (
	"context"
	"fmt"

	"github.com/go-logr/zapr"
	"go.uber.org/zap"

	"github.com/kserve/modelmesh-runtime-adapter/pullman"
	_ "github.com/kserve/modelmesh-runtime-adapter/pullman/storageproviders/http"
)

func main() {
	// set-up the logger
	zaplog, err := zap.NewDevelopment()
	if err != nil {
		panic("Error creating logger...")
	}
	// create a manager
	manager := pullman.NewPullManager(zapr.NewLogger(zaplog))

	// construct the PullCommand
	configJSON := []byte(`{
		"type": "http",
		"url": "http://httpbin.org"
	}`)
	rc := &pullman.RepositoryConfig{}
	_ = json.Unmarshal(configJSON, rc)

	pts := []pullman.Target{
		{
			RemotePath: "uuid",
		},
		{
			RemotePath: "/image/jpeg",
			LocalPath:  "random_image.jpg",
		},
	}

	pc := pullman.PullCommand{
		RepositoryConfig: rc,
		Directory:        "./output",
		Targets:          pts,
	}

	pullErr := manager.Pull(context.Background(), pc)
	if pullErr != nil {
		fmt.Printf("Failed to pull files: %v\n", pullErr)
	}
}
```

Executing the above code results in two files being downloaded:

- a random JPEG image at `output/random_image.jpg`
- a file containing JSON with a uuid at `output/uuid`

## Concepts

### Storage Provider

The `StorageProvider` interface abstracts creating clients to a remote service
that files can be pulled from. For generic providers, a service can be
identified by the communication protocol (s3, http, ftp, etc). A
`StorageProvider` is identified by a string `type`, and available storage
providers are typically registered with PullMan at boot-up via an init
function:

```go
func init() {
	p := Provider{
		// some configurations for the provider
	}
	pullman.RegisterProvider(providerType, p)
}
```

This allows a user of PullMan to control what provider implementations it
makes available. A `StorageProvider` is a factory for `RepositoryClient`s and
creates them from a provider-specific configuration abstracted as a
`Config`.

### Repository Client

A `RepositoryClient` encapsulates the connections to a remote service and
knows how to pull resources from it.

Creating and updating `RepositoryClient` instances can happen dynamically and
asynchronously from pulling any resources. PullMan manages a cache of
`RepositoryClient`s based on requests it has processed and will re-use clients
where possible.

### Pull Command

A `PullCommand` contains all the needed information for PullMan to process a
request to pull resources. Both remote and local resources are identified by
paths. `LocalPath` is a filesystem path, and `RemotePath` is always composed
of segments separated by forward slashes. The `RemotePath` may point to a single
resource or an abstraction pointing to multiple resources (analogous to a
directory). The definition of a "directory" may be different for different
storage providers but must always be compatible with a filesystem path. For
example, an HTTP request that gets a `multipart/form-data` body as a response
could result in writing multiple files when pulling that resource.

```go
// Represents the request to the puller to be fulfilled
type PullCommand struct {
	// repository from which files will be pulled
	RepositoryConfig Config
	// local directory where files will be pulled to
	Directory string
	// the list of targets to be pulled
	Targets []Target
}

type Target struct {
	// remote path to the desired resource(s)
	RemotePath string
	// path to local file to pull the resource to (may have default based on RemotePath)
	LocalPath string
}
```
