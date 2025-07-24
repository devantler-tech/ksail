---
title: Metrics Server
parent: Core Concepts
nav_order: 6
---

# Metrics Server

[Metrics Server](https://github.com/kubernetes-sigs/metrics-server) is a cluster-wide aggregator of resource usage data. It collects metrics from the kubelet on each node and exposes them via the Kubernetes API server. It is used by various Kubernetes components, for purposes such as monitoring and autoscaling.

With KSail you can choose to enable or disable the `Metrics Server` when initializing your project and creating your cluster. The default is to enable the `Metrics Server`, but the underlying distribution may have other defaults. Refer to the below table for the default settings for each distribution:

| Distribution | Metrics Server |
| ------------ | -------------- |
| kind         | Disabled       |
| k3d          | Enabled        |
