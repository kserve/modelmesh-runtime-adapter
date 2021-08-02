# model-serving-puller

This is a component of [model-mesh](https://github.com/kserve/modelmesh) which provides a common way to pull models from storage and hand them off to model runtime frameworks. It implements the model-runtime gRPC API and sits between the mmesh container and the model runtime (adapter) container.
