# Ingress Controllers

Ingress controllers expose HTTP(S) services from inside the cluster. Configure your choice with `ksail cluster init --ingress-controller` or set `spec.networking.ingressController`.

## Default

The `Default` option keeps the controller bundled with the distribution. Kind does not install any ingress by default, while K3d deploys [Traefik](https://doc.traefik.io/traefik/) automatically.

> **Distribution defaults:** Kind ships without an ingress controller; K3d installs Traefik.

## Traefik

Selecting `Traefik` ensures the controller is installed even if the distribution leaves it out. KSail-Go applies the Traefik Helm chart and configures a LoadBalancer service so you can access workloads via host ports.

## None

Use `None` to skip ingress installation entirely. This is helpful when you want to test headless services, deploy an alternative controller, or rely on lightweight port-forwarding during development.
