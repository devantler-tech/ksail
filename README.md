<!-- mcp-name: io.github.devantler-tech/ksail -->

# 🛥️🐳 KSail

KSail helps developers create Kubernetes clusters and deploy applications from one command-line tool.
Kubernetes runs applications packaged in containers; KSail bundles the tools for trying it locally,
then working with cloud providers or an existing cluster.

## Install and try it

On Linux or an Apple Silicon Mac, open a shell with `curl`, `tar` and
`sha256sum` (Linux) or `shasum` (macOS) available:

```sh
curl -fsSL https://github.com/devantler-tech/ksail/releases/latest/download/install.sh | sh
```

The installer verifies the downloaded release's checksum and prints its installation directory.
Follow its `PATH` instruction if needed, so your shell can find `ksail`.
[Other installation options](docs/reader-reference.md#installation-options) include Homebrew and
Windows downloads.

From a directory where you want a new project:

```sh
ksail --help
mkdir ksail-first
cd ksail-first
ksail project init --name ksail-first --distribution Vanilla --provider Docker
```

Expect an initialization success message and `ksail.yaml`, `kind.yaml` and `k8s/kustomization.yaml`
in this directory, plus registry mirror configuration and a `.devcontainer` folder for optional editor setup.
This step only writes project files; it needs no Docker daemon or cloud account.
`--name` names the cluster, not an output directory.

## Run the local cluster

Install and start Docker Engine or Docker Desktop. From the same `ksail-first` directory:

```sh
ksail cluster create
ksail cluster info
ksail cluster delete --name ksail-first --provider Docker
```

Creation downloads images, uses local resources and updates your Kubernetes connection settings.
Check the cluster information, then delete this trial and its workloads when finished.
In your terminal, review the displayed cluster name and type `yes` to confirm deletion.
Project files and cached registry data remain available for reuse.
Cloud providers have separate credentials, setup and costs.

## Next steps

- [Quickstart](https://ksail.devantler.tech/start/quickstart/): deploy your first workload.
- [Reader reference](docs/reader-reference.md): bundled tools, supported platforms, cloud setup and optional AI integrations.
- [Documentation](https://ksail.devantler.tech/): configuration, commands and architecture.
- [Contributing](CONTRIBUTING.md): choose and validate your first change.
- [Discussions](https://github.com/devantler-tech/ksail/discussions) and [issues](https://github.com/devantler-tech/ksail/issues): questions, bugs and ideas.

KSail is source-available under [PolyForm Shield 1.0.0](LICENSE).
Read the license's use, redistribution and noncompetition terms before reusing it.
