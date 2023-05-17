# Copyright 2021 IBM Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# collect args from `make run` so that they don't run twice
ifeq (run,$(firstword $(MAKECMDGOALS)))
  RUN_ARGS := $(wordlist 2,$(words $(MAKECMDGOALS)),$(MAKECMDGOALS))
  ifneq ("$(wildcard /.dockerenv)","")
    $(error Inside docker container, run 'make $(RUN_ARGS)')
  endif
endif

.PHONY: all
## Alias for `build`
all: build

.PHONY: build
## Build runtime docker image
build:
	./scripts/build_docker.sh --target runtime

.PHONY: build.develop
## Build develop docker image
build.develop:
	./scripts/build_docker.sh --target develop

.PHONY: develop
## Build develop docker image and run an interactive shell in the develop envionment
develop: build.develop
	./scripts/develop.sh

.PHONY: run
## Build develop docker image and run a make command in the develop envionment (e.g. `make run fmt` will execute `make fmt` within the docker container)
run: build.develop
	./scripts/develop.sh make $(RUN_ARGS)

.PHONY: test
## Run tests
test:
	./scripts/run_tests.sh

.PHONY: fmt
## Run formatting
fmt:
	./scripts/fmt.sh

.PHONY: proto.compile
## Compile protos
proto.compile:
	./scripts/compile_protos.sh

.PHONY: help
## Print Makefile documentation
help:
	@perl -0 -nle 'printf("%-25s - %s\n", "$$2", "$$1") while m/^##\s*([^\r\n]+)\n^([\w-]+):[^=]/gm' $(MAKEFILE_LIST) | sort
.DEFAULT_GOAL := help

# Override targets if they are included in RUN_ARGs so it doesn't run them twice
$(eval $(RUN_ARGS):;@:)
