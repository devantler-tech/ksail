# GitOps with Flux

**Difficulty:** Intermediate | **Time:** 20 minutes

This example demonstrates automated GitOps workflows using Flux for continuous deployment.

## What You'll Learn

- Bootstrap Flux in a Kubernetes cluster
- Package manifests as OCI artifacts
- Configure automated reconciliation
- Manage GitOps sources and deployments
- Understand drift detection and self-healing

## Prerequisites

- Docker installed and running
- KSail installed
- Basic Kubernetes knowledge
- Understanding of Git workflows

## Architecture

````
┌──────────────┐          ┌──────────────┐
│   Flux       │  watches │  OCI         │
│  Controller  │◄─────────┤  Registry    │
└──────┬───────┘          └──────────────┘
       │
       │ reconciles
       ▼
┌──────────────┐
│  Kubernetes  │
│  Cluster     │
└──────────────┘
````

**Key Concepts:**
- **Source:** OCI artifact containing Kubernetes manifests
- **Reconciliation:** Flux automatically applies changes from the source
- **Drift Detection:** Flux detects and corrects manual changes

## Step-by-Step Guide

### 1. Initialize Cluster with Flux

````bash
# Create cluster with GitOps enabled
ksail cluster create

# Verify Flux is running
kubectl get pods -n flux-system
````

**What Gets Installed:**
- Flux source-controller (manages OCI artifacts)
- Flux kustomize-controller (applies manifests)
- Flux notification-controller (sends alerts)

### 2. Package Manifests as OCI Artifact

````bash
# Build OCI artifact from k8s/ directory
ksail workload gen oci --name hello-gitops --output ./artifacts

# Push to local registry
ksail workload gen oci --name hello-gitops --push
````

**What Happens:**
- Manifests in `k8s/` are packaged into an OCI artifact
- Artifact is pushed to the local registry (localhost:5050)
- Flux can now watch and reconcile from this artifact

### 3. Configure Flux Source

The `flux/` directory contains Flux resources:

- **ocirepository.yaml:** Defines the OCI source
- **kustomization.yaml:** Configures reconciliation

````bash
# Apply Flux configuration
kubectl apply -f flux/

# Verify source is ready
kubectl get ocirepository -n flux-system
````

### 4. Watch Reconciliation

````bash
# Monitor Flux reconciliation
flux get kustomizations

# Check application pods
kubectl get pods -n hello-gitops
````

**Observe:**
- Flux pulls the OCI artifact
- Manifests are applied automatically
- Application starts without manual kubectl apply

### 5. Test Drift Detection

````bash
# Manually scale deployment
kubectl scale deployment hello-gitops -n hello-gitops --replicas=5

# Wait 1 minute - Flux will reconcile back to 2 replicas
watch kubectl get pods -n hello-gitops
````

**What You'll See:**
- Extra pods are terminated
- Flux restores desired state (2 replicas)
- This demonstrates GitOps self-healing

### 6. Update the Application

````bash
# Modify k8s/deployment.yaml (e.g., change replica count to 3)

# Rebuild and push OCI artifact
ksail workload gen oci --name hello-gitops --push

# Flux will detect the change and reconcile automatically
flux reconcile ocirepository hello-gitops -n flux-system

# Verify new replica count
kubectl get pods -n hello-gitops
````

## Cleanup

````bash
ksail cluster delete
````

## File Structure

````
gitops-flux/
├── README.md           # This file
├── ksail.yaml          # KSail configuration (Flux enabled)
├── kind.yaml           # Auto-generated Kind config
├── k8s/                # Application manifests
│   ├── kustomization.yaml
│   ├── namespace.yaml
│   ├── deployment.yaml
│   └── service.yaml
└── flux/               # Flux configuration
    ├── ocirepository.yaml
    └── kustomization.yaml
````

## Key Flux Concepts

### OCI Repository

Flux can watch OCI artifacts for changes:

````yaml
apiVersion: source.toolkit.fluxcd.io/v1beta2
kind: OCIRepository
metadata:
  name: hello-gitops
  namespace: flux-system
spec:
  interval: 1m
  url: oci://localhost:5050/hello-gitops
  ref:
    tag: latest
````

### Kustomization

Defines how Flux applies manifests:

````yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: hello-gitops
  namespace: flux-system
spec:
  interval: 1m
  sourceRef:
    kind: OCIRepository
    name: hello-gitops
  path: ./
  prune: true
  wait: true
````

## Troubleshooting

### Flux Not Reconciling

````bash
# Check Flux logs
flux logs --all-namespaces

# Force reconciliation
flux reconcile ocirepository hello-gitops -n flux-system
flux reconcile kustomization hello-gitops -n flux-system
````

### OCI Artifact Issues

````bash
# Verify registry is running
kubectl get pods -n kube-system | grep registry

# Check artifact was pushed
curl http://localhost:5050/v2/_catalog
````

### Deployment Not Starting

````bash
# Check Kustomization status
kubectl get kustomization hello-gitops -n flux-system -o yaml

# View events
kubectl get events -n hello-gitops --sort-by='.lastTimestamp'
````

## Best Practices

1. **Use semantic versioning** for OCI tags instead of `latest`
2. **Set appropriate intervals** - shorter for dev (1m), longer for prod (5m+)
3. **Enable pruning** to automatically remove deleted resources
4. **Use health checks** with `wait: true` to ensure resources are ready
5. **Monitor Flux alerts** with notification-controller

## Next Steps

- **Multi-Cluster:** Deploy to multiple environments with [Multi-Cluster Management](../multi-cluster/)
- **Secrets Management:** Integrate SOPS with `ksail cipher` commands
- **Advanced Flux:** Explore Helm releases, image automation, and notifications

## Additional Resources

- [Flux Documentation](https://fluxcd.io/docs/)
- [OCI Artifacts](https://fluxcd.io/docs/components/source/ocirepositories/)
- [Kustomize Controller](https://fluxcd.io/docs/components/kustomize/)
- [GitOps Principles](https://www.gitops.tech/)
