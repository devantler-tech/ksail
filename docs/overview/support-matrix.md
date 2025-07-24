---
title: Support Matrix
parent: Overview
nav_order: 2
---

# Support Matrix

KSail aims to support a wide range of use cases by providing the flexibility to run popular Kubernetes distributions in various container engines. Below is a detailed support matrix.

<table>
  <thead>
    <tr>
      <th>Category</th>
      <th>Support</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td><strong>CLI</strong></td>
      <td>
        Linux (amd64 and arm64),<br>
        macOS (amd64 and arm64)
      </td>
    </tr>
    <tr>
      <td><strong>Container Engines</strong></td>
      <td><a href="https://www.docker.com">Docker</a>,
      <a href="https://podman.io">Podman</a></td>
    </tr>
    <tr>
      <td><strong>Distributions</strong></td>
      <td>
        <a href="https://kind.sigs.k8s.io">Kind</a>,
        <a href="https://k3d.io">K3d</a>
      </td>
    </tr>
    <tr>
      <td><strong>Deployment Tools</strong></td>
      <td>
        <a href="https://kubernetes.io/docs/reference/kubectl/">Kubectl</a>,
        <a href="https://fluxcd.io">Flux</a>
      </td>
    </tr>
    <tr>
      <td><strong>Container Network Interfaces (CNI)</strong></td>
      <td>
        Default,
        <a href="https://cilium.io">Cilium</a>,
        None
      </td>
    </tr>
    <tr>
      <td><strong>Container Storage Interfaces (CSI)</strong></td>
      <td>
        Default,
        <a href="https://github.com/rancher/local-path-provisioner">Local Path Provisioner</a>,
        None
      </td>
    </tr>
    <tr>
      <td><strong>Ingress Controllers</strong></td>
      <td>
        Default,
        <a href="https://github.com/traefik/traefik-helm-chart">Traefik</a>,
        None
      </td>
    </tr>
    <tr>
      <td><strong>Gateway Controllers</strong></td>
      <td>
        Default,
        None
      </td>
    </tr>
    <tr>
      <td><strong>Metrics Server</strong></td>
      <td>
        true,
        false
      </td>
    </tr>
      <td><strong>Secret Manager</strong></td>
      <td>
        <a href="https://github.com/getsops/sops">SOPS</a>
      </td>
    </tr>
    <tr>
      <td><strong>Editors</strong></td>
      <td>
        <a href="https://www.nano-editor.org">Nano</a>,
        <a href="https://www.vim.org">Vim</a>
      </td>
    <tr>
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
