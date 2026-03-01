# KSail Examples

This directory contains practical, real-world examples demonstrating how to use KSail for various Kubernetes workflows and scenarios.

## 📚 Available Examples

### 1. **Basic Application Deployment** ([basic-app/](./basic-app/))
**Difficulty:** Beginner | **Time:** 15 minutes

Deploy a simple web application with LoadBalancer service, scaling, and rolling updates.

- ✅ Vanilla (Kind) cluster setup
- ✅ Docker image building
- ✅ Kubernetes deployment and service
- ✅ LoadBalancer configuration
- ✅ Rolling updates and scaling

### 2. **GitOps with Flux** ([gitops-flux/](./gitops-flux/))
**Difficulty:** Intermediate | **Time:** 20 minutes

Set up a complete GitOps workflow using Flux for automated deployments.

- ✅ Flux bootstrap and configuration
- ✅ OCI artifact packaging
- ✅ Automated reconciliation
- ✅ Git-based drift detection
- ✅ Source-to-deployment pipeline

### 3. **Multi-Cluster Management** ([multi-cluster/](./multi-cluster/))
**Difficulty:** Intermediate | **Time:** 25 minutes

Manage development, staging, and production environments with Kustomize overlays.

- ✅ Multiple cluster configurations
- ✅ Kustomize base and overlays
- ✅ Environment-specific settings
- ✅ Context switching
- ✅ Promotion workflows

### 4. **Talos on Hetzner Cloud** ([talos-hetzner/](./talos-hetzner/))
**Difficulty:** Advanced | **Time:** 30 minutes

Provision production-grade Talos clusters on Hetzner Cloud infrastructure.

- ✅ Hetzner provider configuration
- ✅ Talos patches and machine config
- ✅ Load balancer and storage (CSI)
- ✅ Secure boot and encryption
- ✅ Production best practices

### 5. **Development with VCluster** ([vcluster-dev/](./vcluster-dev/))
**Difficulty:** Intermediate | **Time:** 20 minutes

Create isolated virtual Kubernetes clusters for development and testing.

- ✅ VCluster configuration
- ✅ Multi-tenant development
- ✅ Resource isolation
- ✅ Cost-effective testing
- ✅ Rapid environment creation

### 6. **Monitoring Stack** ([monitoring-stack/](./monitoring-stack/))
**Difficulty:** Advanced | **Time:** 35 minutes

Deploy Prometheus, Grafana, and Loki for comprehensive cluster observability.

- ✅ Prometheus operator
- ✅ Grafana dashboards
- ✅ Loki log aggregation
- ✅ AlertManager configuration
- ✅ ServiceMonitor and PodMonitor

## 🚀 Quick Start

Each example directory contains:

- `README.md` - Detailed instructions and explanations
- `ksail.yaml` - KSail configuration file
- `<distribution>.yaml` - Native distribution config (kind.yaml, k3d.yaml, etc.)
- `k8s/` - Kubernetes manifests
- Additional files as needed (Dockerfile, patches, etc.)

To run any example:

````bash
# 1. Navigate to the example directory
cd examples/basic-app/

# 2. Initialize the cluster
ksail cluster create

# 3. Follow the example-specific README
cat README.md
````

## 📖 Learning Path

**New to Kubernetes?** Start here:
1. [Basic Application Deployment](./basic-app/) - Learn core concepts
2. [GitOps with Flux](./gitops-flux/) - Understand declarative workflows

**Familiar with Kubernetes?** Try these:
1. [Multi-Cluster Management](./multi-cluster/) - Scale your workflows
2. [Development with VCluster](./vcluster-dev/) - Optimize development

**Production-Ready Setup?** Advanced examples:
1. [Talos on Hetzner Cloud](./talos-hetzner/) - Cloud deployments
2. [Monitoring Stack](./monitoring-stack/) - Observability

## 🛠️ Prerequisites

All examples require:

- **Docker** installed and running (`docker ps` should work)
- **KSail** installed (see [installation guide](https://ksail.devantler.tech/installation/))

Cloud examples additionally require:

- **Hetzner Cloud API token** (for Talos on Hetzner)
- **Omni service account key** (for Talos on Omni)

## 💡 Tips

- Each example is **self-contained** - you can run them independently
- Examples use **native configuration files** - you can use them with underlying tools (kind, k3d, etc.)
- **Clean up** after each example to avoid conflicts: `ksail cluster delete`
- Examples are **version-controlled** - you can commit them to your repo

## 🤝 Contributing

Found an issue or want to add an example? Contributions are welcome! Please:

1. Follow the existing example structure
2. Include a detailed README with step-by-step instructions
3. Test the example end-to-end before submitting
4. Update this main README with your example

## 📚 Additional Resources

- **Documentation:** <https://ksail.devantler.tech>
- **GitHub Discussions:** <https://github.com/devantler-tech/ksail/discussions>
- **Issue Tracker:** <https://github.com/devantler-tech/ksail/issues>
- **Blog Posts:** <https://devantler.tech/blog/>
