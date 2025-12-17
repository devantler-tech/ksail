# Use Cases

KSail-Go focuses on fast, reproducible feedback loops for Kubernetes projects. Instead of managing production clusters, the CLI targets developer desktops, CI pipelines, and hands-on training sessions where rapid provisioning matters more than long-lived control planes. If you need a refresher on core concepts, start with the [overview](../overview/) and [configuration](../configuration/) sections.

Each scenario builds on the same configuration primitives documented under the [configuration guides](../configuration/). Start with `ksail cluster init` to scaffold a project, commit the resulting YAML, and apply the tips in the guides below to match your workflow.

## Scenarios

- [Learning Kubernetes](learning-kubernetes.md) – Explore distributions, networking options, and kubectl workflows without large infrastructure investments.
- [Local development](local-development.md) – Reproduce production manifests locally, validate changes with `ksail workload reconcile`, and keep the team unblocked.
- [E2E testing in CI/CD](e2e-testing-in-cicd.md) – Spin up ephemeral clusters inside pull-request pipelines to catch regressions before merging.

Have another workflow in mind? Open an issue to share your use case with the community.
