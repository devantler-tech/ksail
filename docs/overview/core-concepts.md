---
title: Core Concepts
parent: Overview
layout: default
nav_order: 0
---

# Core Concepts

<table>
  <thead>
    <tr>
      <th>Concept</th>
      <th>Description</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td><strong>Engines</strong></td>
      <td>In KSail <code>Engines</code> is an abstraction over the underlying provider in which the Kubernetes cluster is spun up.</td>
    </tr>
    <tr>
      <td><strong>Kubernetes Distributions</strong></td>
      <td>In KSail <code>Distributions</code> is an abstraction over the underlying Kubernetes distribution that is used to create the cluster.</td>
    </tr>
    <tr>
      <td><strong>Container Network Interfaces (CNIs)</strong></td>
      <td>In KSail <code>CNIs</code> is an abstraction over the underlying Container Network Interface plugin that is installed in the cluster.</td>
    </tr>
    <tr>
      <td><strong>Ingress Controllers</strong></td>
      <td>In KSail <code>Ingress Controllers</code> is an abstraction over the underlying Ingress Controller that is installed in the cluster.</td>
    </tr>
    <tr>
      <td><strong>Waypoint Controllers</strong></td>
      <td>In KSail <code>Waypoint Controllers</code> is an abstraction over the underlying Waypoint Controller that is installed in the cluster.</td>
    </tr>
    <tr>
      <td><strong>Deployment Tools</strong></td>
      <td>In KSail <code>Deployment Tools</code> is an abstraction over the underlying deployment tool that is used to deploy manifests to the cluster.</td>
    </tr>
    <tr>
      <td><strong>Secret Managers</strong></td>
      <td>In KSail <code>Secret Managers</code> is an abstraction over the underlying secret management tool that is used to manage secrets in Git.</td>
    </tr>
    <tr>
      <td><strong>Local Registry</strong></td>
      <td>In KSail <code>Local Registry</code> is the registry that is used to push and store OCI artifacts locally. It is used as a sync source for GitOps based <code>Deployment Tools</code>.</td>
    </tr>
    <tr>
      <td><strong>Mirror Registries</strong></td>
      <td>In KSail <code>Mirror Registries</code> is the registries used to proxy and cache images from upstream registries. It is used to ensure avoid pull rate limits.</td>
    </tr>
  </tbody>
</table>
