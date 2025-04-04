---
title: Support Matrix
parent: Overview
layout: default
nav_order: 2
---

# Support Matrix

KSail aims to support a wide range of use cases by providing the flexibility to run popular Kubernetes distributions on various local, on-prem, and cloud providers. Below is a detailed support matrix.

<table>
  <thead>
    <tr>
      <th>Category</th>
      <th>Support</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td><strong>Operating Systems</strong></td>
      <td>
        Linux (amd64 and arm64),<br>
        macOS (amd64 and arm64)
      </td>
    </tr>
    <tr>
      <td><strong>Providers</strong></td>
      <td><a href="https://www.docker.com">Docker</a></td>
    </tr>
    <tr>
      <td><strong>Distributions</strong></td>
      <td>
        Native,
        <a href="https://k3d.io">K3s</a>
      </td>
    </tr>
    <tr>
      <td><strong>Deployment Tools</strong></td>
      <td><a href="https://fluxcd.io">Flux</a></td>
    </tr>
    <tr>
      <td><strong>Secret Manager</strong></td>
      <td>
        <a href="https://github.com/getsops/sops">SOPS</a>
      </td>
    </tr>
    <tr>
      <td><strong>Container Network Interfaces (CNI)</strong></td>
      <td>
        Default,
        <a href="https://cilium.io">Cilium</a>
      </td>
    </tr>
    <tr>
      <td><strong>Client-Side Validation</strong></td>
      <td>
        Configuration,
        <a href="https://github.com/aaubry/YamlDotNet">YAML syntax</a>,
        <a href="https://github.com/yannh/kubeconform">Schema </a>
      </td>
    </tr>
  </tbody>
</table>

If you would like to see additional tools supported, please open an issue or pull request on [GitHub](https://github.com/devantler-tech/ksail).
