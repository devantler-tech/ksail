# Development with VCluster

**Difficulty:** Intermediate | **Time:** 20 minutes

This example demonstrates using VCluster for lightweight, isolated development environments.

## What You'll Learn

- Create virtual Kubernetes clusters
- Isolate development workloads
- Share cluster resources efficiently
- Rapid environment creation and deletion
- Cost-effective multi-tenant development

## Why VCluster?

**Traditional Approach:**
- Each developer needs a full cluster
- High resource consumption (CPU, memory)
- Slow cluster creation (minutes)
- Complex multi-tenancy setup

**VCluster Approach:**
- Multiple virtual clusters in one host cluster
- Low resource overhead (virtual control plane only)
- Fast creation (seconds)
- Perfect isolation between virtual clusters

## Architecture

````
┌─────────────────────────────────────────┐
│         Host Cluster (Docker)           │
│                                         │
│  ┌───────────────┐  ┌───────────────┐  │
│  │  VCluster 1   │  │  VCluster 2   │  │
│  │  (dev-alice)  │  │  (dev-bob)    │  │
│  │               │  │               │  │
│  │  - Pods       │  │  - Pods       │  │
│  │  - Services   │  │  - Services   │  │
│  │  - Secrets    │  │  - Secrets    │  │
│  └───────────────┘  └───────────────┘  │
└─────────────────────────────────────────┘
````

## Step-by-Step Guide

### 1. Create VCluster

````bash
# Create virtual cluster
ksail cluster create

# Verify cluster is running
ksail cluster info
````

**What Gets Created:**
- Virtual Kubernetes API server
- Virtual controller manager
- Virtual scheduler
- Syncer (manages workload sync to host)

### 2. Deploy a Workload

````bash
# The kubeconfig is automatically configured for VCluster

# Deploy sample application
ksail workload apply -k ./k8s

# Verify pods are running
kubectl get pods -n demo-app
````

**How It Works:**
- You interact with the virtual API server
- Pods are synced to the host cluster
- Networking and storage are provided by the host
- Full isolation - other VClusters can't see your resources

### 3. Verify Isolation

````bash
# Inside VCluster - you see only your resources
kubectl get namespaces
# Output: default, kube-system, demo-app

# On host cluster - namespaces are prefixed
kubectl get namespaces --context kind-ksail
# Output: ksail-x-default, ksail-x-demo-app, etc.
````

**Key Insight:**
- VCluster provides a complete Kubernetes API
- Resources are transparently synced to the host
- Other VClusters can't access your resources

### 4. Resource Efficiency

````bash
# Check resource usage
docker stats

# Compare:
# - Full Kind cluster: ~2GB RAM, 2 CPUs
# - VCluster: ~500MB RAM, 0.5 CPUs
````

### 5. Multiple VClusters (Optional)

Create multiple isolated environments:

````bash
# Create second VCluster
cd ../vcluster-dev-2
ksail cluster create

# Now you have two isolated environments
ksail cluster list
````

## Cleanup

````bash
# Delete VCluster
ksail cluster delete

# Optionally delete host cluster if done
kind delete cluster --name ksail
````

## File Structure

````
vcluster-dev/
├── README.md           # This file
├── ksail.yaml          # KSail configuration
├── vcluster.yaml       # VCluster configuration (auto-generated)
└── k8s/                # Sample application
    ├── kustomization.yaml
    ├── namespace.yaml
    ├── deployment.yaml
    └── service.yaml
````

## Use Cases

### 1. Developer Environments

Each developer gets an isolated VCluster:

````bash
# Alice's environment
ksail cluster create --name dev-alice

# Bob's environment
ksail cluster create --name dev-bob
````

**Benefits:**
- No interference between developers
- Each can install CRDs, operators, etc.
- Fast creation and deletion

### 2. CI/CD Testing

````bash
# Create test environment for PR
ksail cluster create --name pr-1234

# Run tests
./run-integration-tests.sh

# Cleanup
ksail cluster delete --name pr-1234
````

### 3. Multi-Tenancy

````bash
# Team A environment
ksail cluster create --name team-a

# Team B environment
ksail cluster create --name team-b
````

## VCluster Configuration

The `vcluster.yaml` file controls VCluster behavior:

````yaml
sync:
  services:
    enabled: true
  ingresses:
    enabled: true
  networkpolicies:
    enabled: false

controlPlane:
  distro:
    k8s:
      enabled: true
      version: v1.32.1

networking:
  replicateServices:
    fromHost: []
````

**Key Options:**
- **sync:** Controls what resources are synced to host
- **controlPlane.distro:** Choose K8s, K3s, or K0s
- **networking:** Configure service replication

## LoadBalancer Support

VCluster delegates LoadBalancer services to the host cluster:

````bash
# Create LoadBalancer service in VCluster
kubectl apply -f k8s/service-lb.yaml

# The host cluster provisions the LoadBalancer
kubectl get svc -n demo-app
````

**Note:** The `spec.cluster.loadBalancer` setting in `ksail.yaml` has no effect on VCluster - LoadBalancer provisioning is always handled by the host cluster.

## Troubleshooting

### VCluster Won't Start

````bash
# Check host cluster
kubectl get pods -n vcluster-ksail --context kind-ksail

# View logs
kubectl logs -n vcluster-ksail deployment/ksail --context kind-ksail
````

### Resource Not Syncing

````bash
# Verify sync configuration
cat vcluster.yaml

# Check syncer logs
kubectl logs -n vcluster-ksail -l app=vcluster --context kind-ksail
````

### Kubectl Connection Errors

If you see connection errors immediately after creation, wait 30-60 seconds for the virtual API server to fully initialize, then retry your kubectl commands.

## Best Practices

1. **Use VCluster for development**, not production (use Hetzner/Omni providers for prod)
2. **Delete VClusters when not in use** to free resources
3. **Sync only necessary resources** to reduce overhead
4. **Use host cluster LoadBalancers** for external access
5. **Monitor host cluster resources** to avoid overcommitment

## Next Steps

- **Multi-Cluster:** Scale to multiple environments with [Multi-Cluster Management](../multi-cluster/)
- **GitOps:** Automate deployments with [GitOps with Flux](../gitops-flux/)
- **Production:** Deploy to real infrastructure with [Talos on Hetzner](../talos-hetzner/)

## Additional Resources

- [VCluster Documentation](https://www.vcluster.com/docs)
- [Virtual Clusters Guide](https://www.vcluster.com/docs/what-are-virtual-clusters)
- [KSail VCluster Getting Started](https://ksail.devantler.tech/getting-started/vcluster/)
