# Metrics Server

[Metrics Server](https://github.com/kubernetes-sigs/metrics-server) aggregates CPU and memory usage across the cluster. Enable or disable it with `ksail cluster init --metrics-server` or by setting `spec.cluster.metricsServer`.

> **Distribution defaults:** Kind disables Metrics Server by default; K3d enables it.

When Metrics Server is enabled, KSail-Go installs the upstream Helm chart after cluster creation. For K3d, disabling metrics adds the `--disable=metrics-server` flag to K3s so you preserve parity with production clusters that do not expose metrics by default.

Use metrics when testing Horizontal Pod Autoscaler flows, dashboard tooling, or alerts. Skip it for minimal clusters or when profiling raw resource consumption without the overhead of additional controllers.
