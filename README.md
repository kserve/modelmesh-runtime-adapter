# modelmesh-runtime-adapter

This repo contains the unified puller/runtime-adapter image of the sidecar containers which run in the modelmesh-serving model server Pods. See the main [modelmesh-serving](https://github.com/kserve/modelmesh-serving) repo for more details.

Logical subcomponents within the image:

- [model-serving-puller](model-serving-puller)
- [model-mesh-mlserver-adapter](model-mesh-mlserver-adapter)
- [model-mesh-triton-adapter](model-mesh-triton-adapter)

### Build Image

```bash
make build
```
