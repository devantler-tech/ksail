# Gateway Controllers

Gateway controllers manage [Gateway API](https://gateway-api.sigs.k8s.io) resources and provide a successor to traditional ingress routing. Configure the controller with `ksail cluster init --gateway-controller` or set `spec.networking.gatewayController` in `ksail.yaml`.

> **Note:** Gateway API adoption is still growing. Some controllers ship alpha features and may not be suitable for production traffic.

## Default

`Default` preserves whatever the distribution provides. Today both Kind and K3d do not install a gateway implementation automatically, so you must deploy one manually if you need Gateway API resources.

## None

`None` explicitly disables gateway installation even when a distribution offers one. Use this if you prefer to rely on ingress controllers or want full control over which Gateway implementation is installed later.
