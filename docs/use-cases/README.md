---
title: "Use Cases"
nav_order: 4
has_children: true
---

# Use Cases

KSail focuses on fast, reproducible feedback loops for local Kubernetes development. The CLI targets developer desktops, CI pipelines, and learning environments where rapid provisioning is important.

Each scenario uses the same configuration primitives documented in the [configuration guides](../configuration/). Start with `ksail cluster init` to scaffold a project, then apply the workflows below.

## Scenarios

- [Learning Kubernetes](learning-kubernetes.html) – Explore distributions, networking options, and kubectl workflows
- [Local development](local-development.html) – Work with manifests locally and validate changes before deployment
- [E2E testing in CI/CD](e2e-testing-in-cicd.html) – Spin up ephemeral clusters in pull-request pipelines

> **Note:** Some workflows reference features still being implemented (like full GitOps integration). Check the [support matrix](../overview/support-matrix.html) for current capabilities.
