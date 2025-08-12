---
title: Core Concepts
parent: Overview
nav_order: 0
---

# Core Concepts

> [!TIP]
> If you find KSail hard to use, and understand it is recommended that you familiarize yourself with the listed topics of interest and concepts in KSail. This will help you understand the underlying technologies and concepts that KSail is built on top of, and how they work together to provide a seamless experience.

This guide explores two key areas to help you get the most out of KSail:

1. **Topics of Interest**: These are foundational technologies and methodologies, such as Docker, Kubernetes, and GitOps, that provide the context and knowledge necessary to understand KSail's ecosystem.

2. **Concepts in KSail**: These are the specific abstractions and implementations within KSail itself, like Container Engines, Distributions, and Deployment Tools, which make managing Kubernetes clusters more streamlined and efficient.

By understanding the foundational concepts of cloud-native technologies and how KSail integrates and builds upon them, you'll gain the conceptual knowledge needed to effectively utilize the tool. Let's dive in!

## Topics of Interest

<table>
  <thead>
    <tr>
      <th>Topic</th>
      <th>Description</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td><a href="https://docs.docker.com/">Docker</a></td>
      <td>A platform and container engine for developing, shipping, and running containerized applications.</td>
    </tr>
    <tr>
      <td><a href="https://podman.io/">Podman</a></td>
      <td>A daemonless container engine for developing, managing, and running containers on your system.</td>
    </tr>
    <tr>
      <td><a href="https://kubernetes.io/docs/home/">Kubernetes</a></td>
      <td>An open-source system for automating deployment, scaling, and management of containerized applications.</td>
    </tr>
    <tr>
      <td><a href="https://kubernetes-sigs.github.io/kustomize/">Kustomize</a></td>
      <td>A tool for customizing Kubernetes configurations without modifying the original YAML files.</td>
    </tr>
    <tr>
      <td><a href="https://www.gitops.tech/">GitOps</a></td>
      <td>A methodology for managing infrastructure and application configurations using Git as the source of truth.</td>
    </tr>
    <tr>
      <td><a href="https://www.cncf.io/">Cloud Native</a></td>
      <td>An approach to building and running scalable applications in modern, dynamic environments such as public, private, and hybrid clouds.</td>
    </tr>
  </tbody>
</table>

## Concepts in KSail

<table>
  <thead>
    <tr>
      <th>Concept</th>
      <th>Description</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td><strong>Container Engines</strong></td>
      <td>In KSail <code>Container Engines</code> is an abstraction over the different Container Engines that KSail can spin Kubernetes clusters up on.</td>
    </tr>
    <tr>
      <td><strong>Distributions</strong></td>
      <td>In KSail <code>Distributions</code> is an abstraction over the underlying Kubernetes distribution that is used to create the cluster.</td>
    </tr>
    <tr>
      <td><strong>Container Network Interfaces (CNIs)</strong></td>
      <td><code>CNIs</code> refers to Container Network Interface plugins that facilitate networking for containers in a Kubernetes cluster.</td>
    </tr>
    <tr>
      <td><strong>Container Storage Interfaces (CSIs)</strong></td>
      <td><code>CSIs</code> refers to Container Storage Interface plugins that facilitate storage for containers in a Kubernetes cluster.</td>
    </tr>
    <tr>
      <td><strong>Ingress Controllers</strong></td>
      <td><code>Ingress Controllers</code> refers to the controllers that manage ingress resources in a Kubernetes cluster. They are responsible for routing external traffic to the appropriate services within the cluster.</td>
    </tr>
    <tr>
      <td><strong>Gateway Controllers</strong></td>
      <td><code>Gateway Controllers</code> refers to the controllers that manage gateway resources in a Kubernetes cluster. They are responsible for routing external traffic to the appropriate services within the cluster.</td>
    </tr>
    <tr>
      <td><strong>Deployment Tools</strong></td>
      <td>In KSail <code>Deployment Tools</code> is an abstraction over the underlying deployment tool that is used to deploy manifests to the cluster.</td>
    </tr>
    <tr>
      <td><strong>Secret Manager</strong></td>
      <td>In KSail <code>Secret Manager</code> is SOPS. It is used to work with secrets in the project, and to help keep sensitive values encrypted in Git.</td>
    </tr>
    <tr>
      <td><strong>Local Registry</strong></td>
      <td>In KSail <code>Local Registry</code> is the registry that is used to push and store OCI and Docker images. It is used to store images locally for GitOps based deployment tools, and manually uploaded images.</td>
    </tr>
    <tr>
      <td><strong>Mirror Registries</strong></td>
      <td><code>Mirror Registries</code> refers to registries that are used to proxy and cache images from upstream registries. This is used to avoid pull rate limits and to speed up image pulls.</td>
    </tr>
  </tbody>
</table>
