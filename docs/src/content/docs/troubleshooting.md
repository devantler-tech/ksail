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

1. Open [Hetzner Cloud Console](https://console.hetzner.cloud/)
2. Navigate to your project
3. Go to **Images → ISOs**
4. Find the Talos ISO ID

Configure KSail to use the correct ISO ID (implementation-specific - check latest documentation).

## LoadBalancer Services

### LoadBalancer Service Stuck in Pending

**Symptom:** Service remains in `Pending` state, no external IP assigned

**Possible causes:**

1. **LoadBalancer support disabled** - Distribution does not include LoadBalancer by default
2. **Talos on Docker** - LoadBalancer services are not supported for this combination
3. **cloud-provider-kind not started** - Vanilla (Kind) requires explicit LoadBalancer installation
4. **Controller not running** - LoadBalancer controller pods are crashed or missing

**Solution:**

```bash
# 1. Check service status
ksail workload get svc my-app -o wide
ksail workload describe svc my-app

# 2. Verify LoadBalancer is enabled for your distribution
# Check your ksail.yaml file for loadBalancer setting

# 3. For Vanilla (Kind) - enable cloud-provider-kind
ksail cluster init --distribution Vanilla --load-balancer Enabled
ksail cluster create
# Or update existing cluster:
# Edit ksail.yaml: loadBalancer: Enabled
ksail cluster update

# 4. For K3s - verify ServiceLB pods are running
ksail workload get pods -n kube-system -l svccontroller.k3s.cattle.io/svcname=my-app

# 5. For Talos on Docker - LoadBalancer not supported
# Use NodePort or ClusterIP with port-forwarding instead:
ksail workload port-forward svc/my-app 8080:80

# 6. Check controller logs
# For K3s:
ksail workload logs -n kube-system -l app=svclb-my-app

# For Vanilla (cloud-provider-kind runs as Docker container):
docker logs ksail-cloud-provider-kind

# For Talos on Hetzner:
ksail workload logs -n kube-system daemonset/hcloud-cloud-controller-manager
```

### LoadBalancer IP Address Not Assigned

**Symptom:** Service type is `LoadBalancer` but `EXTERNAL-IP` shows `<none>` or `<pending>`

**Solution:**

```bash
# 1. Verify LoadBalancer support is enabled
cat ksail.yaml | grep loadBalancer

# 2. Check LoadBalancer controller events
ksail workload describe svc my-app | grep -A 10 Events

# 3. For Hetzner Cloud - verify API token and quota
export HCLOUD_TOKEN="your-token"
# Check if you have reached LoadBalancer quota in Hetzner Cloud Console

# 4. For Vanilla (Kind with cloud-provider-kind) - verify controller status
docker ps | grep ksail-cloud-provider-kind
docker logs ksail-cloud-provider-kind | tail -n 50

# 5. Restart LoadBalancer controller
# For K3s:
ksail workload delete pod -n kube-system -l app=svclb-my-app

# For Vanilla (Kind with cloud-provider-kind):
docker restart ksail-cloud-provider-kind

# For Talos×Docker with MetalLB (planned):
# MetalLB is not yet implemented for Talos×Docker
```

### Cloud Provider Load Balancer Errors

**Symptom:** Hetzner Cloud Load Balancer creation fails or shows errors in events

**Solution:**

```bash
# 1. Verify Hetzner Cloud credentials
if [ -z "${HCLOUD_TOKEN:-}" ]; then
  echo "HCLOUD_TOKEN is not set. Please export your Hetzner API token as HCLOUD_TOKEN."
else
  echo "HCLOUD_TOKEN is set. Verify the token value in the Hetzner Cloud Console if issues persist."
fi

# 2. Check cloud controller manager logs
ksail workload logs -n kube-system daemonset/hcloud-cloud-controller-manager --tail=100

# 3. Verify service annotations are valid
ksail workload get svc my-app -o yaml | grep -A 5 annotations

# 4. Check Hetzner Cloud Console for:
#    - Load balancer quota limits
#    - Network/firewall rules
#    - Server location compatibility

# 5. Common annotation issues:
# - Invalid location: use valid Hetzner datacenter (nbg1, fsn1, hel1, etc.)
# - Invalid type: use valid LB type (lb11, lb21, lb31)
# - Network conflicts: ensure cluster network doesn't overlap
```

### Common Misconfigurations

#### Wrong LoadBalancer Setting for Distribution

**Problem:** Using `Enabled` for Talos+Docker or expecting LoadBalancer on Vanilla without enabling it

**Solution:**

```bash
# Vanilla requires explicit LoadBalancer enablement:
ksail cluster init --distribution Vanilla --load-balancer Enabled

# K3s includes LoadBalancer by default (no flag needed):
ksail cluster init --distribution K3s

# Talos on Docker does NOT support LoadBalancer (always Disabled):
# Use NodePort or port-forwarding instead

# Talos on Hetzner includes LoadBalancer by default:
ksail cluster init --distribution Talos --provider Hetzner
```

#### Missing Service Selector

**Problem:** LoadBalancer service has no backend pods

**Solution:**

```bash
# 1. Verify service selector matches pod labels
ksail workload get svc my-app -o yaml | grep -A 3 selector
ksail workload get pods -l app=my-app

# 2. Check endpoints
ksail workload get endpoints my-app
# Should show pod IPs - if empty, selector doesn't match any pods

# 3. Fix selector in service manifest to match pod labels
```

#### Port Conflicts

**Problem:** LoadBalancer port already in use

**Solution:**

```bash
# 1. List all LoadBalancer services
ksail workload get svc -A | grep LoadBalancer

# 2. Check for port conflicts (each service needs unique port)
# For Vanilla with cloud-provider-kind, ports are mapped to host

# 3. Use different ports or NodePort as alternative
```

## Getting More Help

If you're still experiencing issues:

1. **Check existing issues:** [GitHub Issues](https://github.com/devantler-tech/ksail/issues)
2. **Search discussions:** [GitHub Discussions](https://github.com/devantler-tech/ksail/discussions)
3. **Open a new issue:** [New Issue](https://github.com/devantler-tech/ksail/issues/new/choose)

When reporting issues, include:

- KSail version (`ksail --version`)
- Operating system and architecture
- Docker version (`docker --version`)
- Distribution and provider (`Vanilla/K3s/Talos` + `Docker/Hetzner`)
- LoadBalancer configuration (`cat ksail.yaml | grep loadBalancer`)
- Relevant configuration (`ksail.yaml`)
- Service manifest for LoadBalancer issues
- Complete error messages
- Controller logs (see commands above)
- Steps to reproduce
