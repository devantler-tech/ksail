# Local Development

KSail-Go lets you reproduce production-grade manifests on your laptop without hand-crafting cluster plumbing each time.
The CLI leans on your existing container engine (Docker or Podman) and applies the same declarative configuration used
in CI so developers share a consistent workflow. If you need a refresher on configuration precedence or flags, see the
[configuration overview](../configuration/) before starting.

## Day-to-day loop

1. **Scaffold your project**

   ```bash
   ksail cluster init --distribution Kind --source-directory services/api/k8s --cni Cilium --metrics-server true
   ```

   Commit the generated `ksail.yaml`, `kind.yaml`, and baseline manifests so teammates can clone and run the exact configuration.
2. **Start the cluster with dependencies**

   ```bash
   ksail cluster create --wait
   ksail cluster status
   ```

   The `--wait` flag ensures the CNI, metrics stack, and Flux (if enabled) finish reconciling before you deploy workloads.
3. **Build and tag application images**

   ```bash
   docker build -t local.registry.dev/my-app:dev .
   docker push local.registry.dev/my-app:dev
   ```

   Update your manifests to point at the freshly built tag. Use `ksail cluster init --mirror-registries true` when you need registry mirrors for air-gapped setups.
4. **Apply and iterate on workloads**

   ```bash
   ksail workload reconcile -f k8s/overlays/local
   ksail workload diff -f k8s/overlays/local
   ```

   `reconcile` keeps the declarative directory as the source of truth, while `diff` highlights pending changes before they land in the cluster.
5. **Debug quickly**

   ```bash
   ksail workload logs deployment/my-app --container api --tail 200
   ksail workload exec deployment/my-app --container api -- sh
   ```

   Combine KSail-Go helpers with familiar `kubectl` commands for deeper inspection when necessary.
6. **Tear down when finished**

   ```bash
   ksail cluster delete
   ```

   The command removes the Kind cluster and frees local resources so you can start fresh for the next task.

## Tips for faster feedback

- Enable `--metrics-server true` and `--gateway-controller default` when scaffolding to align local observability with higher environments.
- Run `ksail workload gen` to create sample Deployments, Services, or Jobs when prototyping manifests for new components.
- Switch the `distribution` field between `Kind` and `K3d` to mirror the container runtime used in staging.
- Use `ksail cluster connect -- --namespace your-team` to open k9s against the active cluster without remembering kubeconfig paths.

Treat the repository as the contract: commit changes to manifests or KSail configuration to version control so your team inherits the same setup automatically.
