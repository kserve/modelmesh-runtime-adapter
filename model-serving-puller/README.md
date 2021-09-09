# model-serving-puller

This is a component of [ModelMesh Serving](https://github.com/kserve/modelmesh-serving) which provides a common way to pull models from storage and hand them off to model runtime frameworks. It implements the model-runtime gRPC API and sits between the mmesh container and the model runtime container or model runtime adatper within the same process/container.
