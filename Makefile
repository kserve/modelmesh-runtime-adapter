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
## Build runtime Docker image
build:
	./scripts/build_docker.sh --target runtime

.PHONY: build.develop
## Build developer container image
build.develop:
	./scripts/build_docker.sh --target develop

.PHONY: use.develop
## Check if developer image exists, build it if it doesn't
use.develop:
	./scripts/build_docker.sh --target develop --use-existing

.PHONY: develop
## Run interactive shell inside developer container
develop: use.develop
	./scripts/develop.sh

.PHONY: run
## Run make target inside developer container (e.g. `make run fmt`)
run: use.develop
	./scripts/develop.sh make $(RUN_ARGS)

.PHONY: test
## Run tests
test:
	./scripts/run_tests.sh

.PHONY: fmt
## Auto-format source code and report code-style violations (lint)
fmt:
	./scripts/fmt.sh

.PHONY: proto.compile
## Compile protos
proto.compile:
	./scripts/compile_protos.sh

.DEFAULT_GOAL := help
.PHONY: help
## Print Makefile documentation
help:
	@perl -0 -nle 'printf("\033[36m  %-15s\033[0m %s\n", "$$2", "$$1") while m/^##\s*([^\r\n]+)\n^([\w.-]+):[^=]/gm' $(MAKEFILE_LIST) | sort

# Override targets if they are included in RUN_ARGs so it doesn't run them twice
$(eval $(RUN_ARGS):;@:)
