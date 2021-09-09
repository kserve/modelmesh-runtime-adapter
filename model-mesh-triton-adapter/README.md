# Model Mesh Triton Adapter

This is an adapter which implements the internal model-mesh model management API for [Triton Inference Server](https://github.com/triton-inference-server/server).

## How to

1.  Pull Triton Serving Docker Image

        $ docker pull nvcr.io/nvidia/tritonserver:20.09-py3

2.  Run Triton Serving Container with model data mounted

    By default, Triton Serving Docker expose Port `8000` for HTTP and Port `8001` for gRPC.

    Using following command to forward container's `8000` to your workstation's `8000` and container's `8001` to your workstation's `8001`.

        $ docker run -p 8000:8000 -p 8001:8001 -v /Users/tnarayan/AI/model-mesh-triton-adapter/examples/models/:/models nvcr.io/nvidia/tritonserver:20.09-py3 tritonserver --model-store=/models --model-control-mode=explicit --strict-model-config=false --strict-readiness=false

3.  Setup your Golang, gRPC and Protobuff Development Environment locally

    Follow this [gRPC Go Quick Start Guide](https://grpc.io/docs/quickstart/go/)

4.  Run Triton adapter with:

        $ go run main.go

5.  Test adapter with this client from another terminal:

        $ go run triton/client/client.go
