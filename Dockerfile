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
FROM  registry.access.redhat.com/ubi8/ubi-minimal:8.4 AS develop

ARG GOLANG_VERSION=1.17.3
ARG PROTOC_VERSION=3.19.1

USER root

# Install build and dev tools
RUN microdnf install \
    gcc \
    gcc-c++ \
    make \
    vim \
    findutils \
    diffutils \
    git \
    wget \
    tar \
    unzip \
    python3 \
    nodejs && \
    pip3 install pre-commit

# Install go
ENV PATH /usr/local/go/bin:$PATH
RUN set -eux; \
    wget -qO go.tgz "https://golang.org/dl/go${GOLANG_VERSION}.linux-amd64.tar.gz"; \
    sha256sum *go.tgz; \
    tar -C /usr/local -xzf go.tgz; \
    go version

# Install protoc
RUN set -eux; \
    wget -qO protoc.zip "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-x86_64.zip"; \
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

# Don't define an entrypoint. This is a multi-purpose image so the user should specify which binary they want to run (e.g. /opt/app/puller or /opt/app/triton-adapter)
# ENTRYPOINT ["/opt/app/puller"]
