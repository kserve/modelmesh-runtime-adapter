#!/usr/bin/env bash
# Copyright 2023 IBM Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

if ! hash addlicense 2>/dev/null; then
  go install github.com/google/addlicense@latest
  # make sure we can actually use the newly install addlicense script
  if ! command -v addlicense >/dev/null 2>&1; then
    echo "Can not find 'addlicense' after running 'go install github.com/google/addlicense'."
    echo "GOPATH=$GOPATH"
    echo 'Did you forget to export PATH=$PATH:$GOPATH/bin'
    exit 1
  fi
fi

for file in $(find . -type f -name "*.go"); do
  sed -i.bak \
    -e '/\/\/ limitations under the License\.$/{' \
    -e 'N' \
    -e 's/\(\/\/ limitations under the License.\)\n\(package\)/\1\n\n\2/;}' \
    $file
  rm -rf ${file}.bak
done

# add license to new files that does not contains it
shopt -s globstar
addlicense  -v -c "IBM Corporation" -l=apache Dockerfile ./**/*.go scripts

