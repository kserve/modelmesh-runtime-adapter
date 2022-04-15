#!/usr/bin/env bash
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
# limitations under the License.#
set -euo pipefail
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null && pwd )"

function compile_protos {
  package_root="${1%/}"

  go_package_base="github.com/kserve/modelmesh-runtime-adapter"

  proto_dirs="$(find $package_root/* -type d)"
  # include the root directory since it can have protos too
  proto_dirs="${proto_dirs} ${package_root}"

  # construct common opt flags
  protoc_opt_flags=""
  for dir in ${proto_dirs}; do
    protoc_opt_flags="$protoc_opt_flags --proto_path=${dir}"

    # proto file references here need to be relative to the proto_path
    proto_files="$(cd $dir && find . -name '*.proto' -type f)"
    for file in ${proto_files}; do
      # strip leading ./
      file=${file#./}
      # this 'M' opt is an alternative to specifying go_package in the proto
      # files and is required for a proto to correctly import another in the
      # generated code
      protoc_opt_flags="$protoc_opt_flags --go_opt=M${file}=${go_package_base}/${dir}"
      protoc_opt_flags="$protoc_opt_flags --go-grpc_opt=M${file}=${go_package_base}/${dir}"
    done
  done

  # compile each proto directory separately which allows output paths to be set
  # per dir to prevent go package name conflicts
  for dir in ${proto_dirs}; do
    proto_files=$(find $dir -maxdepth 1 -type f -name '*.proto')

    # skip directories without proto files
    if [ -z "$proto_files" ]; then
      continue
    fi

    protoc --go_out=${dir} --go_opt=paths=source_relative \
          --go-grpc_out=${dir} --go-grpc_opt=paths=source_relative \
          ${protoc_opt_flags} \
          ${proto_files}
  done
}

cd "${DIR}/.."

proto_packages_root="internal/proto"
proto_packages=$(ls $proto_packages_root)
for package in ${proto_packages}; do
  package_full_path="${proto_packages_root}/${package}"
  if [[ -d $package_full_path ]]; then
    compile_protos "${package_full_path}"
  fi
done
