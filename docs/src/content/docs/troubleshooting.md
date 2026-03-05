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

```bash
# macOS/Linux
lsof -ti:5000 | xargs kill -9
```

```powershell
# Windows (PowerShell)
netstat -ano | findstr :5000
taskkill /PID <process-id> /F
```

## GitOps Workflow Issues

### Registry Access and Image Push Failures

KSail retries transient registry errors (HTTP 429, 5xx, timeouts) during `cluster create`/`update` automatically. If `ksail workload push` returns authentication errors, verify connectivity and credentials:

```bash
curl -I https://registry.example.com/v2/
docker ps | grep registry
ksail cluster init --local-registry '${REG_USER}:${REG_TOKEN}@registry.example.com/my-org/my-repo'
```

- `registry requires authentication` — missing or incorrect `--local-registry` credentials
- `registry access denied` — credentials lack write permission
- `registry is unreachable` — DNS failure, firewall, or registry down

KSail registry containers (local and mirror) have a Docker-native health check that polls `/v2/` every 10 seconds and marks the container `unhealthy` after 3 consecutive failures. Use this to diagnose registry mirror 500 errors:

```bash
# Check health status — look for (healthy) or (unhealthy) in the STATUS column
docker ps --filter name=registry --format 'table {{.Names}}\t{{.Status}}'

# Inspect detailed health check history for a specific container (raw JSON)
docker inspect --format '{{json .State.Health}}' <container-name>

# Optional: pretty-print the JSON if jq is installed
# docker inspect --format '{{json .State.Health}}' <container-name> | jq .
```

### Flux Operator Installation Timeout

Flux CRDs can take 7–10 minutes on resource-constrained systems; KSail allows up to 12 minutes. If timeouts persist, check resources (`docker stats`) and ensure 4 GB+ RAM.

```bash
ksail workload get pods -n flux-system
kubectl get crd <crd-name> -o jsonpath='{.status.conditions[?(@.type=="Established")].status}'
```

### Flux/ArgoCD CrashLoopBackOff After Component Installation

Infrastructure components (MetalLB, Kyverno, cert-manager) can temporarily disrupt API server connectivity while registering webhooks and CRDs, causing `CrashLoopBackOff` with `dial tcp 10.96.0.1:443: i/o timeout` errors. KSail waits for 3 consecutive successful health checks (2-minute timeout) before installing GitOps engines. If you see `API server not stable after infrastructure installation`:

```bash
ksail workload get nodes
ksail workload get pods -A | grep -v Running
# If needed, disable non-essential components in ksail.yaml and recreate:
ksail cluster delete && ksail cluster create
```

### Flux/ArgoCD Not Reconciling

If changes don't appear after `ksail workload reconcile`, check status and logs:

```bash
ksail workload get pods -n flux-system  # Flux
ksail workload get pods -n argocd       # ArgoCD
ksail workload logs -n flux-system deployment/source-controller
ksail workload reconcile --timeout=5m
```

## Component Installation Issues

### Installation Failures and Timeouts

KSail retries transient Helm registry errors automatically (5 attempts, exponential backoff). For persistent failures or timeouts, check resources with `docker stats` and `curl -I https://ghcr.io`, then recreate the cluster:

```bash
ksail cluster delete && ksail cluster create
```

On resource-constrained systems, increase Docker limits, skip optional components, or use K3s.

## Configuration Issues

### Invalid ksail.yaml

Validate against the [schema](https://github.com/devantler-tech/ksail/blob/main/schemas/ksail-config.schema.json) or re-initialize: `ksail cluster init --name my-cluster --distribution Vanilla`

### Environment Variables Not Expanding

Ensure environment variables are set before running KSail. Verify with `echo $MY_TOKEN` before using `${MY_TOKEN}` in configuration.

## LoadBalancer Issues

### LoadBalancer Service Stuck in Pending

If `kubectl get svc` shows `<pending>` for `EXTERNAL-IP`, verify LoadBalancer is enabled in `ksail.yaml` and check the controller for your distribution:

- **Vanilla**: `docker ps | grep ksail-cloud-provider-kind`
- **Talos**: `kubectl get pods -n metallb-system`
- **Hetzner**: `kubectl get pods -n kube-system | grep hcloud`

If disabled, reinitialize with `--load-balancer Enabled`.

### Cannot Access LoadBalancer IP

If connection fails despite an external IP, ensure the application listens on `0.0.0.0` (not `127.0.0.1`) and check:

```bash
kubectl logs -l app=my-app
kubectl describe svc my-app
kubectl exec -it <pod-name> -- netstat -tlnp
```

### MetalLB IP Pool Exhausted

If new LoadBalancer services remain pending after several successful allocations, the MetalLB IP pool is exhausted. See the [LoadBalancer Configuration Guide](/configuration/loadbalancer/#troubleshooting) to expand the address range.

## Network Issues

### CNI Installation Failed

If pods are stuck in `ContainerCreating` with CNI errors, check CNI pods with `ksail workload get pods -n kube-system -l k8s-app=cilium` (or `calico-node`). If failed, recreate: `ksail cluster init --cni Cilium && ksail cluster create`

## VCluster Issues

### Transient Startup Failures

KSail retries transient VCluster startup failures (up to 5 attempts, 5-second delay), cleaning up partial state and verifying Docker network removal between retries. Retried errors include `exit status 22`/EINVAL, D-Bus errors (recovered in-place), GHCR pull failures, and network transients (timeouts, resets, DNS). Log messages like `Retrying vCluster create (attempt 2/5)...` are expected — no action required.

If all retries fail, check Docker resource limits and D-Bus availability. See the [VCluster Getting Started guide](/getting-started/vcluster/#troubleshooting) for details.

### kubectl Commands Fail After VCluster Creation

If `kubectl get nodes` returns connection errors immediately after VCluster creation, wait a few seconds — VCluster control planes take a moment to become fully ready. Verify the kubeconfig context:

```bash
kubectl config current-context
ksail workload get nodes
```

## Hetzner Cloud Issues

### HCLOUD_TOKEN Not Working

Verify your token has read/write permissions (Hetzner Cloud Console → Security → API Tokens). Test with `hcloud server list` if installed.

### Talos ISO Not Found

The default ISO ID may be outdated. Find the correct ID in [Hetzner Cloud Console](https://console.hetzner.com/) under Images → ISOs.

## Getting More Help

Check [GitHub Issues](https://github.com/devantler-tech/ksail/issues) and [Discussions](https://github.com/devantler-tech/ksail/discussions). When reporting issues, include KSail version, OS, Docker version, `ksail.yaml`, error messages, and reproduction steps.
