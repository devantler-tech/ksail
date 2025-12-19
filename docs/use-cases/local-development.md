---
title: Local Development
parent: Use Cases
nav_order: 2
---

KSail helps you run Kubernetes manifests locally using your container engine (Docker). The CLI provides a consistent workflow that matches your deployment configuration.

## Day-to-day workflow

1. **Scaffold your project**

   ```bash
   ksail cluster init --distribution Kind --source-directory k8s --cni Cilium --metrics-server Enabled
   ```

   Commit the generated `ksail.yaml`, `kind.yaml`, and manifests so teammates can use the same configuration.

2. **Create the cluster**

   ```bash
   ksail cluster create
   ksail cluster info
   ```

   This provisions the cluster with your chosen CNI and metrics-server configuration.

3. **Build and use local images** (if using local registry)

   ```bash
   # Initialize with local registry
   ksail cluster init --local-registry Enabled --local-registry-port 5111
   
   # Build and push
   docker build -t localhost:5111/my-app:dev .
   docker push localhost:5111/my-app:dev
   ```

   Update manifests to reference your local images.

4. **Apply workloads**

   ```bash
   ksail workload apply -k k8s/
   ksail workload get pods
   ```

   Apply your Kustomize manifests and verify deployment.

5. **Debug and inspect**

   ```bash
   ksail workload logs deployment/my-app --tail 200
   ksail workload exec deployment/my-app -- sh
   ksail cluster connect  # Opens k9s
   ```

   Use workload commands or k9s for debugging.

6. **Clean up**

   ```bash
   ksail cluster delete
   ```

   Remove the cluster when finished.

## Tips

- Use `--cert-manager Enabled` during init if you need TLS certificates
- Configure mirror registries with `--mirror-registry` to cache upstream images
- Use `ksail workload gen` to create sample resource manifests
- Test manifests locally before committing to version control
- Switch the `distribution` field between `Kind` and `K3d` to mirror the container runtime used in staging.
- Use `ksail cluster connect -- --namespace your-team` to open k9s against the active cluster without remembering kubeconfig paths.

Treat the repository as the contract: commit changes to manifests or KSail configuration to version control so your team inherits the same setup automatically.
