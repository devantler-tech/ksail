---
title: Troubleshooting
description: Common issues and solutions when using KSail
---

This guide covers common issues you might encounter when using KSail and how to resolve them.

## Cluster Creation Issues

### Docker Connection Failed

**Symptom:** `Error: Cannot connect to the Docker daemon`

**Solution:**

``````bash
# Verify Docker is running
docker ps

# If not running, start Docker Desktop or Docker daemon
# macOS: Open Docker Desktop application
# Linux: sudo systemctl start docker
``````

### Cluster Creation Hangs

**Symptom:** `ksail cluster create` hangs or times out

**Possible causes:**

1. **Insufficient resources** - Check available RAM and CPU
2. **Firewall blocking** - Ensure Docker network access
3. **Previous cluster cleanup** - Delete old clusters first

**Solution:**

``````bash
# Check existing clusters
ksail cluster list --all

# Delete old clusters
ksail cluster delete --name <cluster-name>

# Check Docker resources
docker system df
docker system prune  # Clean up if needed
``````

### Port Already in Use

**Symptom:** `Error: Port 5000 is already allocated` or similar

**Solution:**

``````bash
# Use a different local registry port
ksail cluster init --local-registry-port 5050

# Or find and stop the process using the port
# macOS/Linux:
lsof -ti:5000 | xargs kill -9

# Windows:
netstat -ano | findstr :5000
# Then kill the process ID shown
``````

## Build and Test Issues

### Go Version Mismatch

**Symptom:** `go: module requires Go 1.25.4` or similar

**Solution:**

``````bash
# Check your Go version
go version

# Install/update Go from https://go.dev/dl/
# Or use version manager like gvm or asdf
``````

### Tests Failing with Hetzner Errors

**Symptom:** Tests fail with Hetzner-related errors when running `go test ./...`

**Solution:**

``````bash
# Unset HCLOUD_TOKEN if you're not testing Hetzner features
unset HCLOUD_TOKEN

# Run tests again
go test ./...
``````

## GitOps Workflow Issues

### Image Push Failed

**Symptom:** `ksail workload push` fails with authentication error

**Solution:**

``````bash
# Verify local registry is running
docker ps | grep registry

# Check registry configuration in ksail.yaml
# Ensure authentication is set if using external registry
ksail cluster init \
  --local-registry '${USER}:${TOKEN}@registry.example.com'
``````

### Flux/ArgoCD Not Reconciling

**Symptom:** Changes not appearing in cluster after `ksail workload reconcile`

**Solution:**

``````bash
# Check GitOps controller status
ksail workload get pods -n flux-system  # For Flux
ksail workload get pods -n argocd       # For ArgoCD

# Check reconciliation logs
ksail workload logs -n flux-system deployment/source-controller
ksail workload logs -n flux-system deployment/kustomize-controller

# Force reconciliation
ksail workload reconcile --timeout=5m
``````

## Configuration Issues

### Invalid ksail.yaml

**Symptom:** `Error: invalid configuration file`

**Solution:**

``````bash
# Validate against schema
# The schema is available at:
# https://github.com/devantler-tech/ksail/blob/main/schemas/ksail-config.schema.json

# Re-initialize with correct values
ksail cluster init --name my-cluster --distribution Vanilla
``````

### Environment Variables Not Expanding

**Symptom:** `${VAR}` appears literally in configuration instead of being replaced

**Solution:**

``````bash
# Ensure environment variables are set before running ksail
export MY_TOKEN="secret-value"

# Verify variable is set
echo $MY_TOKEN

# Use the variable in configuration
ksail cluster init \
  --local-registry '${USER}:${MY_TOKEN}@ghcr.io/myorg/myrepo'
``````

## Network Issues

### CNI Installation Failed

**Symptom:** Pods stuck in `ContainerCreating` state, CNI-related errors

**Solution:**

``````bash
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
``````

## Hetzner Cloud Issues

### HCLOUD_TOKEN Not Working

**Symptom:** `Error: invalid token` when creating Talos cluster on Hetzner

**Solution:**

``````bash
# Verify token has correct permissions (read/write)
# Create token in Hetzner Cloud Console: Security → API Tokens

export HCLOUD_TOKEN="your-token-here"

# Test token
hcloud server list  # If you have hcloud CLI installed
``````

### Talos ISO Not Found

**Symptom:** `Error: ISO not found` when creating Talos on Hetzner

**Solution:**

The default ISO ID may be outdated. Check your Hetzner Cloud project:

1. Open [Hetzner Cloud Console](https://console.hetzner.cloud/)
2. Navigate to your project
3. Go to **Images → ISOs**
4. Find the Talos ISO ID

Configure KSail to use the correct ISO ID (implementation-specific - check latest documentation).

## Documentation Build Issues

### npm ci Fails

**Symptom:** `npm ci` fails with dependency errors

**Solution:**

``````bash
# Clear npm cache
npm cache clean --force

# Remove node_modules and package-lock.json
cd docs
rm -rf node_modules package-lock.json

# Reinstall
npm install
``````

### Documentation Build Fails

**Symptom:** `npm run build` fails

**Solution:**

``````bash
# Ensure Node.js version matches CI (v22+)
node --version

# Update Node.js if needed
# Using nvm:
nvm install 22
nvm use 22

# Rebuild documentation
cd docs
npm ci
npm run build
``````

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
