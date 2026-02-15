---
title: Troubleshooting
description: Common issues and solutions when using KSail
---

This guide covers common issues you might encounter when using KSail and how to resolve them.

## Cluster Creation Issues

### Docker Connection Failed

**Symptom:** `Error: Cannot connect to the Docker daemon`

**Solution:**

```bash
# Verify Docker is running
docker ps

# If not running, start Docker Desktop or Docker daemon
# macOS: Open Docker Desktop application
# Linux: sudo systemctl start docker
```

### Cluster Creation Hangs

**Symptom:** `ksail cluster create` hangs or times out

**Possible causes:**

1. **Insufficient resources** - Check available RAM and CPU
2. **Firewall blocking** - Ensure Docker network access
3. **Previous cluster cleanup** - Delete old clusters first

**Solution:**

```bash
# Check existing clusters
ksail cluster list --all

# Delete old clusters
ksail cluster delete --name <cluster-name>

# Check Docker resources
docker system df
docker system prune  # Clean up if needed
```

### Port Already in Use

**Symptom:** `Error: Port 5000 is already allocated` or similar

**Solution:**

```bash
# Use a different local registry port by specifying host:port in --local-registry
ksail cluster init --local-registry http://localhost:5050

# Or find and stop the process using the port
# macOS/Linux:
lsof -ti:5000 | xargs kill -9

# Windows:
netstat -ano | findstr :5000
# Then kill the process ID shown
```

## GitOps Workflow Issues

### Image Push Failed

**Symptom:** `ksail workload push` fails with authentication error

**Solution:**

```bash
# Verify local registry is running
docker ps | grep registry

# Check registry configuration in ksail.yaml
# Ensure authentication is set if using external registry
ksail cluster init \
  --local-registry '${USER}:${TOKEN}@registry.example.com'
```

### Flux Operator Installation Timeout

**Symptom:** Cluster creation hangs during Flux operator installation, or times out after several minutes

**Cause:** On resource-constrained systems (e.g., GitHub Actions runners, low-spec machines), Flux operator CRDs can take 7-10 minutes to become fully "Established" in the API server, even though the operator pod is running.

**Solution:**

KSail automatically handles this with a 12-minute timeout for Flux operator installations. If you still encounter timeouts:

```bash
# Wait for the installation to complete - it may take up to 12 minutes
# Check Flux operator pod status
ksail workload get pods -n flux-system

# Verify CRDs are being established
kubectl get crds | grep fluxcd.io
kubectl get crd <crd-name> -o jsonpath='{.status.conditions[?(@.type=="Established")].status}'
# Should show "True" when ready

# If timeout persists, check system resources
docker stats  # Ensure sufficient CPU/memory
```

For faster installations, ensure your system has adequate resources (4GB+ RAM recommended).

### Flux/ArgoCD Not Reconciling

**Symptom:** Changes not appearing in cluster after `ksail workload reconcile`

**Solution:**

```bash
# Check GitOps controller status
ksail workload get pods -n flux-system  # For Flux
ksail workload get pods -n argocd       # For ArgoCD

# Check reconciliation logs
ksail workload logs -n flux-system deployment/source-controller
ksail workload logs -n flux-system deployment/kustomize-controller

# Force reconciliation
ksail workload reconcile --timeout=5m
```

## Configuration Issues

### Invalid ksail.yaml

**Symptom:** `Error: invalid configuration file`

**Solution:**

```bash
# Validate against schema
# The schema is available at:
# https://github.com/devantler-tech/ksail/blob/main/schemas/ksail-config.schema.json

# Re-initialize with correct values
ksail cluster init --name my-cluster --distribution Vanilla
```

### Environment Variables Not Expanding

**Symptom:** `${VAR}` appears literally in configuration instead of being replaced

**Solution:**

```bash
# Ensure environment variables are set before running ksail
export MY_TOKEN="secret-value"

# Verify variable is set
echo $MY_TOKEN

# Use the variable in configuration
ksail cluster init \
  --local-registry '${USER}:${MY_TOKEN}@ghcr.io/myorg/myrepo'
```

## LoadBalancer Issues

### LoadBalancer Service Stuck in Pending

**Symptom:** Service shows `<pending>` for `EXTERNAL-IP`:

```bash
kubectl get svc
# NAME     TYPE           CLUSTER-IP     EXTERNAL-IP   PORT(S)        AGE
# my-app   LoadBalancer   10.96.1.50     <pending>     80:30123/TCP   5m
```

**Diagnosis:**

1. **Check if LoadBalancer is enabled:**

   ```bash
   # Check ksail.yaml
   cat ksail.yaml | grep -A 5 "loadBalancer"
   ```

2. **Verify LoadBalancer controller is running:**

   **Vanilla (Cloud Provider KIND):**

   ```bash
   docker ps | grep ksail-cloud-provider-kind
   # Should show a container named ksail-cloud-provider-kind
   ```

   **Talos (MetalLB):**

   ```bash
   kubectl get pods -n metallb-system
   # Should show controller and speaker pods in Running state
   ```

   **Hetzner:**

   ```bash
   kubectl get pods -n kube-system | grep hcloud
   # Should show hcloud-cloud-controller-manager pod
   ```

**Solution:**

```bash
# If LoadBalancer is disabled, re-initialize cluster with LoadBalancer enabled
ksail cluster delete
ksail cluster init --name my-cluster --load-balancer Enabled
ksail cluster create

# For Cloud Provider KIND issues, delete and recreate cluster
# For MetalLB issues, check if IP pool is exhausted (see below)
# For Hetzner issues, ensure HCLOUD_TOKEN was set during cluster creation
```

### Cannot Access LoadBalancer IP

**Symptom:** Service has external IP but connection fails:

```bash
curl http://172.18.255.200
# curl: (7) Failed to connect to 172.18.255.200 port 80: Connection refused
```

**Diagnosis:**

1. **Verify pods are running:**

   ```bash
   kubectl get pods -l app=my-app
   ```

2. **Check service endpoints:**

   ```bash
   kubectl get endpoints my-app
   # Should show pod IPs
   ```

3. **Test from within cluster:**

   ```bash
   kubectl run test --rm -it --image=curlimages/curl -- sh
   curl http://my-app.default.svc.cluster.local
   ```

**Solution:**

```bash
# Wait for pods to reach Running state
kubectl get pods -l app=my-app -w

# Verify target port matches container port
kubectl describe svc my-app

# Check pod logs for application errors
kubectl logs -l app=my-app

# Verify application listens on 0.0.0.0, not 127.0.0.1
kubectl exec -it <pod-name> -- netstat -tlnp
```

### MetalLB IP Pool Exhausted

**Symptom:** New LoadBalancer services remain pending after several successful allocations

**Solution:**

Expand the IP range by creating a new pool with additional addresses:

```yaml
# expanded-pool.yaml
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: expanded-pool
  namespace: metallb-system
spec:
  addresses:
    - 172.18.255.200-172.18.255.254 # Expanded from .250 to .254
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: expanded-l2
  namespace: metallb-system
spec:
  ipAddressPools:
    - expanded-pool
```

```bash
kubectl apply -f expanded-pool.yaml
```

For more LoadBalancer troubleshooting, see the [LoadBalancer Configuration Guide](/configuration/loadbalancer/#troubleshooting).

## Network Issues

### CNI Installation Failed

**Symptom:** Pods stuck in `ContainerCreating` state, CNI-related errors

**Solution:**

```bash
# Check CNI pods are running
ksail workload get pods -n kube-system

# For Cilium
ksail workload get pods -n kube-system -l k8s-app=cilium

# For Calico
ksail workload get pods -n kube-system -l k8s-app=calico-node

# Recreate cluster with specific CNI
ksail cluster delete
ksail cluster init --cni Cilium
ksail cluster create
```

## Hetzner Cloud Issues

### HCLOUD_TOKEN Not Working

**Symptom:** `Error: invalid token` when creating Talos cluster on Hetzner

**Solution:**

```bash
# Verify token has correct permissions (read/write)
# Create token in Hetzner Cloud Console: Security → API Tokens

export HCLOUD_TOKEN="your-token-here"

# Test token
hcloud server list  # If you have hcloud CLI installed
```

### Talos ISO Not Found

**Symptom:** `Error: ISO not found` when creating Talos on Hetzner

**Solution:**

The default ISO ID may be outdated. Check your Hetzner Cloud project:

1. Open [Hetzner Cloud Console](https://console.hetzner.com/)
2. Navigate to your project
3. Go to **Images → ISOs**
4. Find the Talos ISO ID

Configure KSail to use the correct ISO ID (implementation-specific - check latest documentation).

## Getting More Help

If you're still experiencing issues:

1. **Check existing issues:** [GitHub Issues](https://github.com/devantler-tech/ksail/issues)
2. **Search discussions:** [GitHub Discussions](https://github.com/devantler-tech/ksail/discussions)
3. **Open a new issue:** [New Issue](https://github.com/devantler-tech/ksail/issues/new/choose)

When reporting issues, include:

- KSail version (`ksail --version`)
- Operating system and architecture
- Docker version (`docker --version`)
- Relevant configuration (`ksail.yaml`)
- Complete error messages
- Steps to reproduce
