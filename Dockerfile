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
# Stage 1: Create the develop, test, and build environment
###############################################################################
FROM registry.access.redhat.com/ubi8/go-toolset:1.17 AS develop

ARG GOLANG_VERSION=1.17.13
ARG PROTOC_VERSION=21.5

USER root

# Install build and dev tools
RUN dnf install -y --nodocs \
#    gcc \
#    gcc-c++ \
#    make \
#    vim \
#    findutils \
#    diffutils \
#    git \
#    wget \
#    tar \
#    unzip \
    python3 python3-pip\
    nodejs && \
    pip3 install pre-commit

# https://docs.docker.com/engine/reference/builder/#automatic-platform-args-in-the-global-scope
# - TARGETPLATFORM - e.g. linux/amd64, linux/arm/v7, windows/amd64
# - TARGETOS       - e.g. linux, windows, darwin
# - TARGETARCH     - e.g. amd64, arm32v7, arm64v8, i386, ppc64le, s390x
ARG TARGETOS=linux
ARG TARGETARCH=amd64

# Verify go version, TODO: remove
#ENV PATH /usr/local/go/bin:$PATH
RUN set -eux; \
    echo "PATH=$PATH"; \
    go version

# Install protoc
# The protoc download files use a different variation of architecture identifiers
# from the Docker TARGETARCH forms amd64, arm64, ppc64le, s390x
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
#   TARGETARCH=amd64
#   amd64=x86_64
#   echo ${!TARGETARCH}
RUN set -eux; \
    amd64=x86_64; \
    arm64=aarch_64; \
    ppc64le=ppcle_64; \
    s390x=s390_64; \
    wget -qO protoc.zip "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-${TARGETOS}-${!TARGETARCH}.zip"; \
    sha256sum protoc.zip; \
    unzip protoc.zip -x readme.txt -d /usr/local; \
    protoc --version

# Install go protoc plugins
ENV PATH /root/go/bin:$PATH
RUN go get google.golang.org/protobuf/cmd/protoc-gen-go \
           google.golang.org/grpc/cmd/protoc-gen-go-grpc

WORKDIR /opt/app

# Download and initialize the pre-commit environments before copying the source so they will be cached
COPY .pre-commit-config.yaml ./
RUN git init && \
    pre-commit install-hooks && \
    rm -rf .git

# Download dependiencies before copying the source so they will be cached
COPY go.mod go.sum ./
RUN go mod download


###############################################################################
# Stage 2: Run the build
###############################################################################
FROM develop AS build

LABEL image="build"

# Copy the source
COPY . ./

# Build the binaries
RUN go build -o puller model-serving-puller/main.go
RUN go build -o triton-adapter model-mesh-triton-adapter/main.go
RUN go build -o mlserver-adapter model-mesh-mlserver-adapter/main.go
RUN go build -o ovms-adapter model-mesh-ovms-adapter/main.go
RUN go build -o torchserve-adapter model-mesh-torchserve-adapter/main.go

###############################################################################
# Stage 3: Copy build assets to create the smallest final runtime image
###############################################################################
FROM  registry.access.redhat.com/ubi8/ubi-minimal:8.4 as runtime

ARG IMAGE_VERSION
ARG COMMIT_SHA
ARG USER=2000

LABEL name="model-serving-runtime-adapter" \
      version="${IMAGE_VERSION}" \
      release="${COMMIT_SHA}" \
      summary="Sidecar container which runs in the Model-Mesh Serving model server pods" \
      description="Container which runs in each model serving pod and act as an intermediary between model-mesh and third-party model-server containers"

USER root
# install python to convert keras to tf
RUN microdnf install \
    gcc \
    gcc-c++ \
    python38 && \
    ln -sf /usr/bin/python3 /usr/bin/python && \
    ln -sf /usr/bin/pip3 /usr/bin/pip && \
    pip install tensorflow

USER ${USER}

# Copy over the binary and use it as the entrypoint
COPY --from=build /opt/app/puller /opt/app/
COPY --from=build /opt/app/triton-adapter /opt/app/
COPY --from=build /opt/app/mlserver-adapter /opt/app/
COPY --from=build /opt/app/model-mesh-triton-adapter/scripts/tf_pb.py /opt/scripts/
COPY --from=build /opt/app/ovms-adapter /opt/app/
COPY --from=build /opt/app/torchserve-adapter /opt/app/


# Don't define an entrypoint. This is a multi-purpose image so the user should specify which binary they want to run (e.g. /opt/app/puller or /opt/app/triton-adapter)
# ENTRYPOINT ["/opt/app/puller"]
