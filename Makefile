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

all: build

build:
	./scripts/build_docker.sh --target runtime

build.develop:
	./scripts/build_docker.sh --target develop

develop: build.develop
	./scripts/develop.sh

run: build.develop
	./scripts/develop.sh make $(RUN_ARGS)

test:
	./scripts/run_tests.sh

fmt:
	./scripts/fmt.sh

proto.compile:
	./scripts/compile_protos.sh

# Override targets if they are included in RUN_ARGs so it doesn't run them twice
$(eval $(RUN_ARGS):;@:)

# Remove $(MAKECMDGOALS) if you don't intend make to just be a taskrunner
.PHONY: all $(MAKECMDGOALS)
