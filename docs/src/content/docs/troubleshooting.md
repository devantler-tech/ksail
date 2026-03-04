---
title: Troubleshooting
description: Common issues and solutions when using KSail
---

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
# Windows (PowerShell)
netstat -ano | findstr :5000
taskkill /PID <process-id> /F
```

## GitOps Workflow Issues

### Registry Access and Image Push Failures

KSail verifies registry access during `ksail cluster create`/`update` and retries transient errors (HTTP 429, 5xx, timeouts) automatically. If verification fails, or if `ksail workload push` returns authentication errors, verify the registry is reachable and credentials are configured:

```bash
curl -I https://registry.example.com/v2/   # test connectivity
docker ps | grep registry                  # verify local registry is running
ksail cluster init --local-registry '${REG_USER}:${REG_TOKEN}@registry.example.com/my-org/my-repo'
```

Common errors:

- **"registry requires authentication"** — missing or incorrect credentials in `--local-registry`
- **"registry access denied"** — credentials lack write permission
- **"registry is unreachable"** — DNS resolution failure, firewall, or registry down

### Flux Operator Installation Timeout

On resource-constrained systems, Flux operator CRDs can take 7-10 minutes to become established. KSail uses a 12-minute timeout to handle this automatically. If timeouts persist, check system resources with `docker stats` and ensure 4GB+ RAM is available.

```bash
ksail workload get pods -n flux-system
kubectl get crd <crd-name> -o jsonpath='{.status.conditions[?(@.type=="Established")].status}'
```

### Flux/ArgoCD CrashLoopBackOff After Component Installation

Infrastructure components (MetalLB, Kyverno, cert-manager, etc.) temporarily destabilize API server connectivity while registering webhooks and CRDs, causing Flux or ArgoCD to enter `CrashLoopBackOff` with `dial tcp 10.96.0.1:443: i/o timeout` errors.

KSail automatically waits for 3 consecutive successful health checks (2-minute timeout) before installing GitOps engines. If you see `API server not stable after infrastructure installation`:

```bash
ksail workload get nodes
ksail workload get pods -A | grep -v Running

# Disable non-essential components in ksail.yaml, then recreate
ksail cluster delete
ksail cluster create
```

### Flux/ArgoCD Not Reconciling

If changes don't appear after `ksail workload reconcile`, check controller status and logs:

```bash
ksail workload get pods -n flux-system  # Flux
ksail workload get pods -n argocd       # ArgoCD
ksail workload logs -n flux-system deployment/source-controller
ksail workload reconcile --timeout=5m
```

## Component Installation Issues

### Transient Installation Failures

Helm registries occasionally return 429 or 5xx errors. KSail retries automatically (5 attempts, exponential backoff). For persistent failures:

```bash
ksail cluster delete && ksail cluster create
curl -I https://registry.example.com
```

### Component Installation Timeout

Timeouts typically result from insufficient resources, network latency, or large chart artifacts. Monitor with `docker stats` and `curl -I https://ghcr.io`. On resource-constrained systems, increase Docker limits, skip optional components, or use K3s.

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

If `ksail cluster create` fails with `exit status 22` (EINVAL) or D-Bus errors, KSail retries automatically (3 attempts, 5s delay). Messages like `Retrying vCluster create (attempt 2/3)...` are expected — no action required.

If all retries fail, check Docker resource limits and D-Bus availability. See the [VCluster Getting Started guide](/getting-started/vcluster/#troubleshooting).

### kubectl Commands Fail After VCluster Creation

If `kubectl get nodes` returns connection errors immediately after creating a VCluster, wait a few seconds and retry. VCluster control planes take a moment to become fully ready. Verify the kubeconfig context is correct:

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
