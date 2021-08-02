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

USAGE="$(
  cat <<EOF
Build your "docs" folder into html
usage: $0 [flags]
  [-h | --help]     Display this help
  [-d | --dev]      Run a dev server to see docs locally
  [-c | --command]  Run a command in doc builder image
EOF
)"

usage() {
  echo "$USAGE" >&2
  exit 1
}

function echoError() {
  LIGHT_YELLOW='\033[1;33m'
  NC='\033[0m' # No Color

  if [ "${CI}" != "true" ]; then
    echo -e "${LIGHT_YELLOW}${1}${NC}"
  else
    echo -e "[ERROR] ${1}"
  fi
}

COMMAND=""

while (("$#")); do
  arg="$1"
  case $arg in
  -h | --help)
    usage
    ;;
  -d | --dev)
    COMMAND="yarn start"
    shift 1
    ;;
  -c | --command)
    if [ -n "$2" ] && [ "${2:0:1}" != "-" ]; then
      COMMAND=$2
      shift 2
    else
      echo "Error: Argument for $1 is missing" >&2
      usage
    fi
    ;;
  -* | --*=) # unsupported flags
    echo "Error: Unsupported flag $1" >&2
    usage
    ;;

  esac
done

IMAGE_REPO="wcp-ai-foundation-team-docker-virtual.artifactory.swg-devops.com"
IMAGE_NAME="gatsby-theme-foundation-docs"
IMAGE_TAG="latest"

docker pull "${IMAGE_REPO}/${IMAGE_NAME}:${IMAGE_TAG}"
RETURN_CODE=$?

if [ $RETURN_CODE -ne 0 ]; then
    echoError "Are you logged in to Artifactory?"
    echoError "Fill in and run: \`docker login ${IMAGE_REPO} -u <your_email> -p \$ARTIFACTORY_API_KEY\`"
    echoError 'Get an API Key from: https://na.artifactory.swg-devops.com/ui/admin/artifactory/user_profile'

    exit $RETURN_CODE
fi

docker run -it --rm \
  -p 8000:8000 \
  -v "$(pwd)/docs:/opt/app/packages/${IMAGE_NAME}/src/pages" \
  -v "$(pwd)/public:/public" \
  -p 34242:34242 \
  -e INTERNAL_STATUS_PORT=34242 \
  "${IMAGE_REPO}/${IMAGE_NAME}:${IMAGE_TAG}" $COMMAND 
