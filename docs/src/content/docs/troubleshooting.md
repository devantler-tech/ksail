---
title: Troubleshooting
description: Common issues and solutions when using KSail
---

This guide covers common issues you might encounter when using KSail and how to resolve them.

## Cluster Creation Issues

### Docker Connection Failed

Verify Docker is running with `docker ps`. If not running, start Docker Desktop (macOS) or `sudo systemctl start docker` (Linux).

### Cluster Creation Hangs

Common causes include insufficient resources, firewall blocking Docker network access, or previous clusters not cleaned up.

```bash
# Check and cleanup existing clusters
ksail cluster list
ksail cluster delete --name <cluster-name>

# Clean up Docker resources if needed
docker system df
docker system prune -f
```

### Port Already in Use

If you encounter `Error: Port 5000 is already allocated`, either configure a different local registry address (for example, `--local-registry localhost:5050`) or kill the process currently using the port:

```bash
# macOS/Linux
lsof -ti:5000 | xargs kill -9
```

```powershell
# Windows: find the PID, then kill it
netstat -ano | findstr :5000
taskkill /PID <process-id> /F
```

## GitOps Workflow Issues

### Registry Access Verification Failed

During `ksail cluster create` and `ksail cluster update`, KSail verifies access to the configured external registry before proceeding. Transient network errors (including timeouts, HTTP 429, and 5xx responses) are automatically retried with exponential backoff (up to 3 attempts with 2s then 4s delays between attempts, each attempt using a 30s timeout).

If verification consistently fails, check credentials and connectivity:

```bash
# Test registry connectivity
curl -I https://registry.example.com/v2/

# (Optional) Manually verify credentials; KSail does not use Docker's credential store
docker login registry.example.com

# Configure KSail with registry credentials via env vars (recommended to avoid leaking tokens)
# Example:
#   export REG_USER='your-username'
#   export REG_TOKEN='your-access-token'
ksail cluster init --local-registry '${REG_USER}:${REG_TOKEN}@registry.example.com/my-org/my-repo'
```

Common errors and causes:

- **"registry requires authentication"** — missing or incorrect credentials in `--local-registry`
- **"registry access denied"** — credentials lack write permission to the repository
- **"registry is unreachable"** — DNS resolution failure, firewall block, or registry is down

### Image Push Failed

If `ksail workload push` fails with authentication errors, verify the local registry is running with `docker ps | grep registry`.

KSail automatically reads credentials from `~/.docker/config.json`. Authenticate once with `docker login registry.example.com` or set environment variables:

```bash
export REGISTRY_USER="myuser"
export REGISTRY_TOKEN="mytoken"
ksail cluster init --local-registry '${REGISTRY_USER}:${REGISTRY_TOKEN}@registry.example.com'
```

### Flux Operator Installation Timeout

On resource-constrained systems, Flux operator CRDs can take 7-10 minutes to become established. KSail uses a 12-minute timeout to handle this automatically. If timeouts persist, check system resources with `docker stats` and ensure 4GB+ RAM is available.

```bash
# Check Flux operator pod status
ksail workload get pods -n flux-system

# Verify CRDs are established (should show "True")
kubectl get crd <crd-name> -o jsonpath='{.status.conditions[?(@.type=="Established")].status}'
```

### Flux/ArgoCD Not Reconciling

If changes don't appear after `ksail workload reconcile`, check controller status and logs:

```bash
# Check status
ksail workload get pods -n flux-system  # Flux
ksail workload get pods -n argocd       # ArgoCD

# Check logs
ksail workload logs -n flux-system deployment/source-controller

# Force reconciliation
ksail workload reconcile --timeout=5m
```

## Component Installation Issues

### Transient Installation Failures (Rate Limits)

Helm chart registries may return 429, 500, or 503 errors during high load. KSail automatically retries installations with exponential backoff (5 attempts, 3-30s delays). Most transient failures resolve automatically.

If all retries fail, recreate the cluster or check network connectivity to registries. KSail caches Helm repository indexes in CI environments to improve reliability.

```bash
# For persistent failures
ksail cluster delete && ksail cluster create

# Check connectivity
docker ps && curl -I https://registry.example.com
```

### Component Installation Timeout

Component installation timeouts typically result from insufficient resources, network latency, or large chart artifacts. Monitor resources with `docker stats` and test connectivity to registries with `curl -I https://ghcr.io`.

For resource-constrained systems, increase Docker resource limits, skip optional components, or use the K3s distribution (lighter than Vanilla).

## Configuration Issues

### Invalid ksail.yaml

Validate against the [schema](https://github.com/devantler-tech/ksail/blob/main/schemas/ksail-config.schema.json) or re-initialize: `ksail cluster init --name my-cluster --distribution Vanilla`

### Environment Variables Not Expanding

Ensure environment variables are set before running KSail. Verify with `echo $MY_TOKEN` before using `${MY_TOKEN}` in configuration.

## LoadBalancer Issues

### LoadBalancer Service Stuck in Pending

If `kubectl get svc` shows `<pending>` for `EXTERNAL-IP`, verify LoadBalancer is enabled in `ksail.yaml` and check controller status:

```bash
# Vanilla: docker ps | grep ksail-cloud-provider-kind
# Talos: kubectl get pods -n metallb-system
# Hetzner: kubectl get pods -n kube-system | grep hcloud
```

If disabled, re-initialize: `ksail cluster init --name my-cluster --load-balancer Enabled`

### Cannot Access LoadBalancer IP

If connection fails despite having an external IP, verify pods are running (`kubectl get pods -l app=my-app`), check service endpoints (`kubectl get endpoints my-app`), and ensure the application listens on `0.0.0.0`, not `127.0.0.1`.

```bash
# Check logs and port configuration
kubectl logs -l app=my-app
kubectl describe svc my-app
kubectl exec -it <pod-name> -- netstat -tlnp
```

### MetalLB IP Pool Exhausted

If new LoadBalancer services remain pending after several successful allocations, expand the IP range:

```yaml
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: expanded-pool
  namespace: metallb-system
spec:
  addresses:
    - 172.18.255.200-172.18.255.254 # Expand as needed
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: expanded-l2
  namespace: metallb-system
spec:
  ipAddressPools: [expanded-pool]
```

See the [LoadBalancer Configuration Guide](/configuration/loadbalancer/#troubleshooting) for more details.

## Network Issues

### CNI Installation Failed

If pods are stuck in `ContainerCreating` with CNI errors, check CNI pods are running with `ksail workload get pods -n kube-system -l k8s-app=cilium` (or `calico-node` for Calico). If failed, recreate the cluster: `ksail cluster init --cni Cilium && ksail cluster create`

## VCluster Issues

### Transient Startup Failures

**Symptom:** `ksail cluster create` fails with `exit status 22` (EINVAL) or similar D-Bus errors on CI runners.

KSail automatically retries transient VCluster startup failures with up to 3 attempts and a 5-second delay between attempts, cleaning up partial state between retries. If you see log messages like `Retrying vCluster create (attempt 2/3)...`, this is expected behavior — no action is required.

If all retries fail, check Docker resource limits and D-Bus availability on the runner. See the [VCluster Getting Started guide](/getting-started/vcluster/#troubleshooting) for detailed steps.

### kubectl Commands Fail After VCluster Creation

**Symptom:** `kubectl get nodes` returns connection errors immediately after creating a VCluster.

VCluster control planes can take a moment to become fully ready. Wait a few seconds and retry, or check that the kubeconfig context is set correctly:

```bash
kubectl config current-context
ksail workload get nodes
```

## Hetzner Cloud Issues

### HCLOUD_TOKEN Not Working

Verify your token has read/write permissions. Create tokens in Hetzner Cloud Console under Security → API Tokens. Test with `hcloud server list` if the CLI is installed.

### Talos ISO Not Found

The default ISO ID may be outdated. Find the correct ID in [Hetzner Cloud Console](https://console.hetzner.com/) under Images → ISOs, then configure KSail accordingly.

## Getting More Help

Check [GitHub Issues](https://github.com/devantler-tech/ksail/issues) and [Discussions](https://github.com/devantler-tech/ksail/discussions). When reporting issues, include KSail version, OS, Docker version, `ksail.yaml`, error messages, and reproduction steps.
