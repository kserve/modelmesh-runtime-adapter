#!/bin/bash
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

set -e

## declare an array variable
declare -a subpackages=("model-mesh-mlserver-adapter"
                        "model-mesh-ovms-adapter"
                        "model-mesh-torchserve-adapter"
                        "model-mesh-triton-adapter"
                        "model-serving-puller"
                        "pullman")

failed=false
summary=""
for i in "${subpackages[@]}"
do
    echo
    echo "--------------------------------------------------------"
    echo "Running tests for $i"
    echo "--------------------------------------------------------"
    if ! $i/scripts/run_tests.sh; then
        failed=true
        status="FAIL"
    else
        status="PASS"
    fi
    summary="${summary}\n${status}: ${i}"
done

echo -e "\n\n"
echo "--------------------------------------------------------"
echo -n "Summary after running tests for ${#subpackages[@]} sub-packages:"
echo -e "$summary"
echo "--------------------------------------------------------"

if $failed; then
    exit 1
fi
