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

###############################################################################
# Stage 1: Create the developer image for the BUILDPLATFORM only
###############################################################################
ARG GOLANG_VERSION=1.17
FROM --platform=$BUILDPLATFORM registry.access.redhat.com/ubi8/go-toolset:$GOLANG_VERSION AS develop

ARG PROTOC_VERSION=21.5

USER root
ENV HOME=/root

# Install build and dev tools
RUN --mount=type=cache,target=/root/.cache/dnf:rw \
    dnf install --setopt=cachedir=/root/.cache/dnf -y --nodocs \
        python3 \
        python3-pip \
        nodejs \
    && true

# Install pre-commit
ENV PIP_CACHE_DIR=/root/.cache/pip
RUN --mount=type=cache,target=/root/.cache/pip \
    pip3 install pre-commit

# When using the BuildKit backend, Docker predefines a set of ARG variables with
# information on the platform of the node performing the build (build platform)
# These arguments are defined in the global scope but are not automatically available
# inside build stages. We need to expose the BUILDOS and BUILDARCH inside the build
# stage and redefine it without a value
# https://docs.docker.com/engine/reference/builder/#automatic-platform-args-in-the-global-scope
ARG BUILDOS
ARG BUILDARCH

# Install protoc
# The protoc download files use a different variation of architecture identifiers
# from the Docker BUILDARCH forms amd64, arm64, ppc64le, s390x
#   protoc-22.2-linux-aarch_64.zip  <- arm64
#   protoc-22.2-linux-ppcle_64.zip  <- ppc64le
#   protoc-22.2-linux-s390_64.zip   <- s390x
#   protoc-22.2-linux-x86_64.zip    <- amd64
# so we need to map the arch identifiers before downloading the protoc.zip using
# shell parameter expansion: with the first character of a parameter being an
# exclamation point (!) it introduces a level of indirection where the value
# of the parameter is used as the name of another variable and the value of that
# other variable is the result of the expansion, e.g. the echo statement in the
# following three lines of shell script print "x86_64"
#   BUILDARCH=amd64
#   amd64=x86_64
#   echo ${!BUILDARCH}
RUN set -eux; \
    amd64=x86_64; \
    arm64=aarch_64; \
    ppc64le=ppcle_64; \
    s390x=s390_64; \
    wget -qO protoc.zip "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-${BUILDOS}-${!BUILDARCH}.zip" \
    && sha256sum protoc.zip \
    && unzip protoc.zip -x readme.txt -d /usr/local \
    && protoc --version \
    && true

WORKDIR /opt/app

COPY go.mod go.sum ./

# Install go protoc plugins
ENV PATH $HOME/go/bin:$PATH
RUN go get google.golang.org/protobuf/cmd/protoc-gen-go \
           google.golang.org/grpc/cmd/protoc-gen-go-grpc \
    && protoc-gen-go --version \
    && true

# Download and initialize the pre-commit environments before copying the source so they will be cached
COPY .pre-commit-config.yaml ./
RUN git init && \
    pre-commit install-hooks && \
    rm -rf .git

# Download dependencies before copying the source so they will be cached
RUN go mod download

# the ubi/go-toolset image doesn't define ENTRYPOINT or CMD, but we need it to run 'make develop'
CMD /bin/bash


###############################################################################
# Stage 2: Run the go build with BUILDPLATFORM's native go compiler
###############################################################################
FROM --platform=$BUILDPLATFORM develop AS build

LABEL image="build"

# Copy the source
COPY . ./

# https://docs.docker.com/engine/reference/builder/#automatic-platform-args-in-the-global-scope
# don't provide "default" values (e.g. 'ARG TARGETARCH=amd64') for non-buildx environments,
# see https://github.com/docker/buildx/issues/510
ARG TARGETOS
ARG TARGETARCH

# Build the binaries using native go compiler from BUILDPLATFORM but compiled output for TARGETPLATFORM
# https://www.docker.com/blog/faster-multi-platform-builds-dockerfile-cross-compilation-guide/
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \
    export GOOS=${TARGETOS:-linux} && \
    export GOARCH=${TARGETARCH:-amd64} && \
    go build -o puller model-serving-puller/main.go && \
    go build -o triton-adapter model-mesh-triton-adapter/main.go && \
    go build -o mlserver-adapter model-mesh-mlserver-adapter/main.go && \
    go build -o ovms-adapter model-mesh-ovms-adapter/main.go && \
    go build -o torchserve-adapter model-mesh-torchserve-adapter/main.go


###############################################################################
# Stage 3: Copy build assets to create the smallest final runtime image
###############################################################################
FROM registry.access.redhat.com/ubi8/ubi-minimal:8.7 as runtime

ARG USER=2000

USER root

USER ${USER}

# Copy over the binary and use it as the entrypoint
COPY --from=build /opt/app/puller /opt/app/
COPY --from=build /opt/app/triton-adapter /opt/app/
COPY --from=build /opt/app/mlserver-adapter /opt/app/
COPY --from=build /opt/app/model-mesh-triton-adapter/scripts/tf_pb.py /opt/scripts/
COPY --from=build /opt/app/ovms-adapter /opt/app/
COPY --from=build /opt/app/torchserve-adapter /opt/app/

# wait to create commit-specific LABEL until end of the build to not unnecessarily
# invalidate the cached image layers
ARG IMAGE_VERSION
ARG COMMIT_SHA

LABEL name="model-serving-runtime-adapter" \
      version="${IMAGE_VERSION}" \
      release="${COMMIT_SHA}" \
      summary="Sidecar container which runs in the ModelMesh Serving model server pods" \
      description="Container which runs in each model serving pod acting as an intermediary between ModelMesh and third-party model-server containers"

# Don't define an entrypoint. This is a multi-purpose image so the user should specify which binary they want to run (e.g. /opt/app/puller or /opt/app/triton-adapter)
# ENTRYPOINT ["/opt/app/puller"]
