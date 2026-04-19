---
title: Troubleshooting
description: Common issues and solutions when using KSail
---

## Cluster Creation Issues

### Docker Connection Failed

Verify Docker is running with `docker ps`. If not running, start Docker Desktop (macOS) or `sudo systemctl start docker` (Linux).

### Cluster Creation Hangs

Common causes: insufficient resources, firewall blocking Docker network access, or leftover cluster state.

```bash
ksail cluster list
ksail cluster delete --name <cluster-name>
docker system prune -f
```

### Port Already in Use

If you see `Error: Port 5000 is already allocated`, use a different port (e.g., `--local-registry localhost:5050`) or kill the conflicting process:

**macOS/Linux:**

```bash
lsof -ti:5000 | xargs kill -9
```

**Windows (PowerShell):**

```powershell
netstat -ano | findstr :5000
taskkill /PID <id> /F
```

## GitOps Workflow Issues

### Registry Access and Image Push Failures

KSail automatically retries transient registry errors (HTTP 429, 5xx, timeouts) during cluster create/update and `ksail workload push` (up to 5 attempts, exponential backoff 5s–30s). For authentication errors, verify connectivity and credentials:

```bash
curl -I https://registry.example.com/v2/
docker ps | grep registry
ksail cluster init --local-registry '${REG_USER}:${REG_TOKEN}@registry.example.com/my-org/my-repo'
```

- `external registry credentials are incomplete: username is set but password is empty` — a username was provided (e.g. `GITHUB_ACTOR` is set) but the password/token is missing. Export the token environment variable (e.g. `export GITHUB_TOKEN=...`) and ensure both are set in `spec.cluster.localRegistry.registry` in `ksail.yaml`, or re-initialize with `ksail cluster init --local-registry 'user:token@host/repo'`.
- `registry requires authentication` — missing or incorrect `--local-registry` credentials
- `registry access denied` — credentials lack write permission
- `registry is unreachable` — DNS failure, firewall, or registry down

Registry containers have a built-in health check (polls `/v2/` every 10 s, marks `unhealthy` after 3 consecutive failures). To diagnose mirror errors:

```bash
docker ps --filter label=io.ksail.registry --format 'table {{.Names}}\t{{.Status}}'
docker inspect --format '{{json .State.Health}}' <container-name>
```

### Flux Operator Installation Timeout

Flux CRDs can take 7–10 minutes on resource-constrained systems; KSail allows up to 12 minutes. If timeouts persist, check resources (`docker stats`) and ensure 4 GB+ RAM.

```bash
ksail workload get pods -n flux-system
kubectl get crd <crd-name> -o jsonpath='{.status.conditions[?(@.type=="Established")].status}'
```

### Cluster Stability Check Failures

KSail performs cluster stability checks at two points during installation to prevent race conditions:

- **Before infrastructure components** (Cilium CNI only): Ensures the Cilium eBPF dataplane has finished programming pod-to-service routing before deploying components like metrics-server that depend on ClusterIP connectivity.
- **Before GitOps engines**: Always runs before Flux or ArgoCD are installed to ensure the API server is fully ready. This is especially important for K3s/K3d clusters, which report creation success before the API server is ready to serve requests. On setups with infrastructure components (MetalLB, Kyverno, cert-manager), it also ensures API connectivity has recovered after those components register webhooks and CRDs.

Each check performs up to three steps: (1) 5 consecutive successful API server health checks, (2) all kube-system DaemonSets ready, and (3) a short-lived busybox pod confirms TCP connectivity to the API server ClusterIP (Cilium CNI only). The in-cluster connectivity check allows up to **3 minutes for VCluster** (vs 2 minutes for other distributions), since VCluster + Cilium eBPF needs extra time to stabilize atop the host cluster's network layer.

If you see `cluster not stable before infrastructure installation`, `cluster not stable after infrastructure installation`, or `in-cluster API connectivity check failed`, check resources and optionally recreate with fewer components:

```bash
ksail workload get nodes
ksail workload get pods -A | grep -v Running
ksail cluster delete && ksail cluster create
```

If the error mentions `connectivity check pod image pull failed` with `ImagePullBackOff` or `ErrImagePull`, the connectivity check pod could not pull its `busybox:stable` image. This is typically a transient Docker Hub rate-limit or network issue — not an actual API server connectivity failure. Verify Docker Hub reachability (`curl -I https://registry-1.docker.io/v2/`) and retry with `ksail cluster delete && ksail cluster create`. If a docker.io mirror registry is configured, ensure it is healthy and reachable from within the cluster.

### Flux/ArgoCD Not Reconciling

If changes don't appear after `ksail workload reconcile`, check status and logs:

```bash
ksail workload get pods -n flux-system  # Flux
ksail workload get pods -n argocd       # ArgoCD
ksail workload logs -n flux-system deployment/source-controller
ksail workload reconcile --timeout=5m
```

## Image Export Issues

### Blob Integrity Check Failed

After `ksail workload export`, KSail validates the SHA256 digest of every blob in the exported OCI tar archive. If a blob is truncated or corrupt — which `ctr export` can produce silently when containerd's content store has incomplete data (e.g., from an interrupted image pull or runner resource pressure) — you will see an error like:

```text
blob integrity check failed: blob blobs/sha256/<hex>: computed SHA256 <actual> (read N of M bytes)
```

or

```text
blob integrity check failed: tar archive is truncated or corrupted: ...
```

**Resolution**: The containerd content store on the node has an incomplete or corrupt blob for one of the exported images. Pull a fresh copy of the affected image locally and import it into the cluster to replace the corrupt data, then re-export:

```bash
# Pull a fresh copy of the affected image into your local Docker daemon
docker pull <image>
# Save it to a tar archive
docker save <image> -o fresh.tar
# Import the fresh image into the cluster
ksail workload import fresh.tar
# Re-export
ksail workload export
```

If the error spans multiple images or you cannot identify the affected image from the blob SHA, recreate the cluster to force a full re-pull of all images:

```bash
ksail cluster delete && ksail cluster create
ksail workload export
```

## Component Installation Issues

### Installation Failures and Timeouts

KSail retries transient Helm registry errors automatically (5 attempts, exponential backoff). For persistent failures, check resources with `docker stats` and `curl -I https://ghcr.io`, then recreate: `ksail cluster delete && ksail cluster create`. On resource-constrained systems, increase Docker limits, skip optional components, or use K3s.

## Configuration Issues

### Invalid ksail.yaml

Validate against the [schema](https://github.com/devantler-tech/ksail/blob/main/schemas/ksail-config.schema.json) or re-initialize: `ksail cluster init --name my-cluster --distribution Vanilla`

### Environment Variables Not Expanding

Ensure environment variables are set before running KSail. Verify with `echo $MY_TOKEN` before using `${MY_TOKEN}` in configuration.

## LoadBalancer Issues

### LoadBalancer Service Stuck in Pending

If `kubectl get svc` shows `<pending>` for `EXTERNAL-IP`, verify LoadBalancer is enabled in `ksail.yaml` (reinitialize with `--load-balancer Enabled` if not) and check the controller for your distribution:

- **Vanilla**: `docker ps | grep ksail-cloud-provider-kind`
- **Talos**: `kubectl get pods -n metallb-system`
- **Hetzner**: `kubectl get pods -n kube-system | grep hcloud`

### Cannot Access LoadBalancer IP

If connection fails despite an external IP, ensure the application listens on `0.0.0.0` (not `127.0.0.1`). Debug with `kubectl logs -l app=my-app`, `kubectl describe svc my-app`, and `kubectl exec -it <pod-name> -- netstat -tlnp` to check listening ports.

### MetalLB IP Pool Exhausted

If new LoadBalancer services remain pending after several successful allocations, the MetalLB IP pool is exhausted. See the [LoadBalancer Configuration Guide](/configuration/loadbalancer/#troubleshooting) to expand the address range.

## Network Issues

### CNI Installation Failed

If pods are stuck in `ContainerCreating` with CNI errors, check CNI pods with `ksail workload get pods -n kube-system -l k8s-app=cilium` (or `calico-node`). If failed, recreate: `ksail cluster init --cni Cilium && ksail cluster create`

## Talos Issues

### Transient Image Pull Failures

KSail automatically retries transient Talos node image pull failures (up to 3 attempts, exponential backoff 5s–30s) to handle network glitches from `ghcr.io` (e.g., 504 Gateway Timeout). `Talos image pull attempt N failed (retrying in Xs): ...` messages are expected — no action required.

If all retries fail, check your internet connection and `ghcr.io` availability with `curl -I https://ghcr.io/v2/`, then retry with `ksail cluster delete && ksail cluster create`.

## VCluster Issues

### Transient Startup Failures

KSail automatically retries transient VCluster startup failures (up to 5 attempts, 5-second delay), including exit status 22/EINVAL, D-Bus errors, network transients, GHCR pull failures, and node join timeouts (kubelet TLS bootstrap). `Retrying vCluster create (attempt 2/5)...` messages are expected — no action required.

If all retries fail, check Docker resource limits and D-Bus availability. See the [VCluster guide](/distributions/vcluster/#troubleshooting) for details.

### kubectl Commands Fail After VCluster Creation

Wait a few seconds if `kubectl get nodes` returns connection errors immediately after creation — VCluster control planes need time to start. Verify the active context with `kubectl config current-context` and `ksail workload get nodes`.

## Hetzner Cloud Issues

- **HCLOUD_TOKEN not working**: Verify read/write permissions (Hetzner Cloud Console → Security → API Tokens). Test with `hcloud server list` if installed.
- **Talos ISO not found**: The default ISO ID may be outdated. Find the correct ID in [Hetzner Cloud Console](https://console.hetzner.com/) under Images → ISOs.

## Getting More Help

Check [GitHub Issues](https://github.com/devantler-tech/ksail/issues) and [Discussions](https://github.com/devantler-tech/ksail/discussions). When reporting issues, include KSail version, OS, Docker version, `ksail.yaml`, error messages, and reproduction steps.
