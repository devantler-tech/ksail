# Basic Application Deployment

**Difficulty:** Beginner | **Time:** 15 minutes

This example demonstrates the fundamental workflow of deploying a simple web application to a Kubernetes cluster using KSail.

## What You'll Learn

- Create a local Kubernetes cluster with KSail
- Build and load Docker images
- Deploy a web application with Kubernetes manifests
- Expose services via LoadBalancer
- Scale deployments
- Perform rolling updates

## Prerequisites

- Docker installed and running
- KSail installed (see [installation guide](https://ksail.devantler.tech/installation/))
- Basic understanding of Docker and Kubernetes concepts

## Application Overview

We'll deploy a simple Node.js web server that:

- Serves HTTP requests on port 3000
- Responds with a greeting and version number
- Demonstrates rolling update capabilities

## Step-by-Step Guide

### 1. Initialize the Cluster

````bash
# Create the cluster with KSail
ksail cluster create

# Verify the cluster is running
ksail cluster info
````

**Expected Output:**
````
✅ Cluster 'ksail' is running
📊 Distribution: Vanilla (Kind)
🐳 Provider: Docker
📦 Nodes: 1 control-plane, 0 workers
````

### 2. Build and Load the Application Image

````bash
# Build the Docker image
docker build -t hello-ksail:v1 .

# Load the image into the cluster
ksail cluster import-images hello-ksail:v1
````

**What's Happening:**
- `docker build` creates a container image from the Dockerfile
- `ksail cluster import-images` loads the image into the cluster's image cache
- This avoids pulling from external registries during development

### 3. Deploy the Application

````bash
# Apply Kubernetes manifests
ksail workload apply -k ./k8s

# Watch the deployment progress
kubectl get pods -w
````

**What Gets Created:**
- **Deployment:** Manages 2 replicas of the hello-ksail pod
- **Service:** LoadBalancer type exposing port 80
- **Namespace:** `hello-ksail` for resource isolation

### 4. Access the Application

````bash
# Get the LoadBalancer IP
export LB_IP=$(kubectl get svc hello-ksail -n hello-ksail -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# Test the application
curl http://$LB_IP
````

**Expected Response:**
````json
{
  "message": "Hello from KSail!",
  "version": "1.0.0",
  "hostname": "hello-ksail-7d4f8c5b9-xyz12"
}
````

### 5. Scale the Application

````bash
# Scale to 5 replicas
kubectl scale deployment hello-ksail -n hello-ksail --replicas=5

# Verify scaling
kubectl get pods -n hello-ksail
````

**Observe:**
- New pods are created automatically
- LoadBalancer distributes traffic across all replicas
- Each response shows a different pod hostname

### 6. Perform a Rolling Update

````bash
# Update the image tag in k8s/deployment.yaml
# Change: image: hello-ksail:v1
# To:     image: hello-ksail:v2

# Build the new version
docker build -t hello-ksail:v2 --build-arg VERSION=2.0.0 .

# Load into cluster
ksail cluster import-images hello-ksail:v2

# Apply the update
ksail workload apply -k ./k8s

# Watch the rolling update
kubectl rollout status deployment/hello-ksail -n hello-ksail
````

**What's Happening:**
- Kubernetes gradually replaces old pods with new ones
- No downtime - old pods remain until new ones are ready
- Traffic automatically shifts to the new version

### 7. Verify the Update

````bash
# Test the updated application
curl http://$LB_IP
````

**Expected Response:**
````json
{
  "message": "Hello from KSail!",
  "version": "2.0.0",
  "hostname": "hello-ksail-9f5a2e3d1-abc45"
}
````

## Cleanup

````bash
# Delete the cluster
ksail cluster delete

# Confirm deletion
ksail cluster list
````

## File Structure

````
basic-app/
├── README.md           # This file
├── ksail.yaml          # KSail cluster configuration
├── kind.yaml           # Kind cluster configuration (auto-generated)
├── Dockerfile          # Application container image
├── server.js           # Node.js web server
├── package.json        # Node.js dependencies
└── k8s/                # Kubernetes manifests
    ├── kustomization.yaml
    ├── namespace.yaml
    ├── deployment.yaml
    └── service.yaml
````

## Troubleshooting

### Pods Not Starting

````bash
# Check pod status
kubectl get pods -n hello-ksail

# View pod logs
kubectl logs -n hello-ksail -l app=hello-ksail

# Describe pod for events
kubectl describe pod -n hello-ksail -l app=hello-ksail
````

### LoadBalancer IP Not Assigned

For Vanilla (Kind) clusters, KSail automatically configures Cloud Provider KIND for LoadBalancer support. If the IP is not assigned:

````bash
# Check cloud-provider-kind container
docker ps | grep cloud-provider-kind

# Verify service status
kubectl get svc -n hello-ksail hello-ksail -o yaml
````

### Image Pull Errors

````bash
# Verify image is loaded
docker exec -it ksail-control-plane crictl images | grep hello-ksail

# Reload if necessary
ksail cluster import-images hello-ksail:v1
````

## Key Concepts

### LoadBalancer Services

KSail configures LoadBalancer support automatically:

- **Vanilla (Kind):** Uses Cloud Provider KIND
- **K3s (K3d):** Built-in service LB (Klipper)
- **Talos:** MetalLB or cloud provider integration
- **VCluster:** Delegates to host cluster

### Image Management

````bash
# Import specific image
ksail cluster import-images <image>:<tag>

# Import multiple images
ksail cluster import-images image1:v1 image2:v2

# List images in cluster
docker exec -it ksail-control-plane crictl images
````

### Workload Deployment

````bash
# Apply manifests with kubectl
ksail workload apply -k ./k8s

# Or use GitOps workflow (if Flux/ArgoCD enabled)
ksail workload reconcile
````

## Next Steps

- **GitOps Workflow:** Try the [GitOps with Flux](../gitops-flux/) example to automate deployments
- **Multi-Cluster:** Manage multiple environments with [Multi-Cluster Management](../multi-cluster/)
- **Observability:** Add monitoring with the [Monitoring Stack](../monitoring-stack/) example

## Additional Resources

- [KSail Documentation](https://ksail.devantler.tech)
- [Kubernetes Deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)
- [Service Types](https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services-service-types)
- [Rolling Updates](https://kubernetes.io/docs/tutorials/kubernetes-basics/update/update-intro/)
