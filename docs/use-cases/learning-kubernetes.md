---
title: "Learning Kubernetes"
parent: Use Cases
nav_order: 1
---

# Learning Kubernetes

KSail simplifies Kubernetes experimentation by wrapping Kind and K3d with a consistent interface. Focus on learning Kubernetes concepts without complex cluster setup.

## Quick start session

1. **Scaffold a playground**

   ```bash
   ksail cluster init --distribution Kind --source-directory k8s
   ```

   Edit `ksail.yaml` or rerun with different flags to try distributions, CNIs, or metrics-server options.

2. **Create a cluster**

   ```bash
   ksail cluster create
   ```

   KSail installs your chosen CNI and metrics-server automatically.

3. **Try common workloads**

   ```bash
   ksail workload gen deployment echo --image=hashicorp/http-echo:0.2.3 --port 5678
   ksail workload apply -f echo.yaml
   ksail workload wait --for=condition=Available deployment/echo --timeout=120s
   ```

   Experiment with generators and manual YAML edits to learn Kustomize.

4. **Inspect the cluster**

   Use `ksail cluster connect` to launch k9s, or explore with `kubectl get all -A`.

5. **Reset quickly**

   ```bash
   ksail cluster delete
   ```

   Recreate as often as needed.

## Learning tips

- Switch between `--distribution Kind` and `--distribution K3d` to compare runtimes
- Try `--cni Cilium` during init to explore advanced networking
- Use `--cert-manager Enabled` to learn about TLS certificate management
- Track configuration in Git to understand how changes affect cluster behavior

Reference `kubectl explain` and the [Kubernetes documentation](https://kubernetes.io/docs/) for deeper understanding.
