# Learning Kubernetes

KSail-Go removes the heavy lifting required to experiment with Kubernetes. By wrapping Kind and K3d behind a consistent interface you can focus on objects, workloads, and reconciliation instead of cluster plumbing. Review the [configuration reference](../configuration/) if you want a deeper explanation of the fields mentioned in this guide.

## Quick start session

1. **Scaffold a playground**

   ```bash
   ksail cluster init --distribution Kind --source-directory lab/k8s
   ```

   Edit the generated `ksail.yaml` or rerun the command with different flags to compare distributions, CNIs, or metrics-server behavior.
2. **Create a cluster**

   ```bash
   ksail cluster create
   ```

   KSail-Go installs your chosen CNI and metrics stack automatically, so you can move straight to workloads.
3. **Try common workloads**

   ```bash
   ksail workload gen deployment echo --image=hashicorp/http-echo:0.2.3 --port 5678
   ksail workload apply -f echo.yaml
   ksail workload wait --for=condition=Available deployment/echo --timeout=120s
   ```

   Pair the generators with manual YAML edits to see how Kustomize overlays affect resources.
4. **Inspect the cluster**
   Launch k9s with `ksail cluster connect -- --namespace default`, or explore via `kubectl get all -A`.
5. **Reset quickly**

   ```bash
   ksail cluster delete
   ```

   Recreate the environment as often as needed without touching production infrastructure.

## Tips for deeper dives

- Switch between `--distribution Kind` and `--distribution K3d` to understand how different runtimes expose networking.
- Use `--cni Cilium` or `--cni Calico` during `ksail cluster init` to watch how CNIs alter namespace defaults and available CRDs.
- Add Flux by enabling the Flux option in `ksail.yaml` and experiment with GitOps reconciliation via `ksail workload reconcile`.
- Track configuration changes with Git—rollback to any commit to see how the cluster behaves with previous settings.

When questions pop up, reference `kubectl explain`, the [Kubernetes documentation](https://kubernetes.io/docs/), or open a discussion in the KSail-Go repository.
