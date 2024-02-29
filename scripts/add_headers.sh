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
fi

# adds a new line between the header and the package keyword when it does not exists.
# we use sed to make in-file text replacements, but sed works differently depending on the version
if ! sed -i '1s/^/test/' $(mktemp) 2> /dev/null; then
    # macOS (BSD) version of sed
    alias gsed="sed -i ''"
else
    # POSIX compliant version of sed
    alias gsed="sed -i"
fi
export gsed

find . -type f -name "*.go" -exec gsed -e '/\/\/ limitations under the License\.$/{' -e 'N' -e 's/\(\/\/ limitations under the License.\)\n\(package\)/\1\n\n\2/;}' {} \;

# add license to new files that does not contains it
shopt -s globstar
"${GOPATH}"/bin/addlicense  -v -c "IBM Corporation" -l=apache Dockerfile ./**/*.go scripts

