# Model Mesh OpenVINO Model Server (OVMS)

This is an adapter which implements the internal model-mesh model management API for [OpenVINO Model Server](https://github.com/openvinotoolkit/model_server).

This adapter is different than the other adapters because OpenVINO is modeled after TensorflowServing and does not implement the KFSv2 API. OVMS also does not have a direct model management API; its multimodel support is implemented with an on-disk config file that can be reloaded with a REST API call.
