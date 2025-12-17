# Project Structure

Running `ksail cluster init` scaffolds a repository that is immediately compatible with the KSail-Go command set. The exact layout varies depending on flags such as `--distribution`, `--gateway-controller`, and declarative overrides in [`ksail.yaml`](../configuration/declarative-config.md), but every project starts with the same core folders:

```text
├── ksail.yaml              # Declarative cluster definition consumed by ksail-go
├── kind.yaml / k3d.yaml     # Distribution-specific overrides generated during init
└── k8s/                     # GitOps-ready manifests (bases, overlays, and Flux wiring)
    └── kustomization.yaml   # Root Kustomize entrypoint referenced by workload commands
```

When the `--secret-manager SOPS` option is enabled (or `spec.project.secretManager` is set in `ksail.yaml`), KSail-Go also adds a `.sops.yaml` file and a `keys/` directory stub so the `ksail cipher` commands have an opinionated home for Age recipients. See the [configuration docs](../configuration/#when-to-edit-what) for guidance on managing these files in version control.

## Organizing with Kustomize

KSail-Go embraces a [Kustomize](https://kustomize.io/) first architecture. The `k8s/kustomization.yaml` file generated at init time becomes the anchor for both local iterations and GitOps automation:

- **Bases and overlays** – Declarative configuration from `ksail.yaml` is rendered into Kustomize bases so you can patch provider-specific differences without copy/paste.
- **Flux integration** – Optional Flux manifests are generated under `k8s/flux/` and referenced from the root kustomization, allowing you to bootstrap GitOps reconciliations with `ksail workload reconcile`.
- **Configurable entrypoint** – Use `ksail cluster init --kustomization-path <path>` or set `spec.project.kustomizationPath` to change which file becomes the default overlay.

Because the CLI loads the same manifests that Flux consumes, every change you make locally can be validated with `ksail workload apply` or `ksail workload reconcile` before it reaches CI/CD. The [local development playbook](../use-cases/local-development.md) walks through that feedback loop end-to-end.
