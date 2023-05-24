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
all: build

.PHONY: build
build:
	./scripts/build_docker.sh --target runtime

.PHONY: build.develop
build.develop:
	./scripts/build_docker.sh --target develop

.PHONY: develop
develop: build.develop
	./scripts/develop.sh

.PHONY: run
run: build.develop
	./scripts/develop.sh make $(RUN_ARGS)

.PHONY: test
test:
	./scripts/run_tests.sh

.PHONY: fmt
fmt:
	./scripts/fmt.sh

.PHONY: proto.compile
proto.compile:
	./scripts/compile_protos.sh

# Override targets if they are included in RUN_ARGs so it doesn't run them twice
$(eval $(RUN_ARGS):;@:)
