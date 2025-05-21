---
title: FAQ
layout: default
nav_order: 4
---

# Frequently Asked Questions

> [!NOTE]
> If you have a question that is not answered here, please [open an issue](https://github.com/devantler-tech/ksail/issues/new). I will do my best to answer it and add it to this list if it occurs frequently

- [How do I configure local DNS?](#how-do-i-configure-local-dns)
  - [Option 1: Use `traefik.me` to resolve `*.traefik.me` to 127.0.0.1](#option-1-use-traefikme-to-resolve-traefikme-to-127001)
  - [Options 2: Configure your `/etc/hosts` file](#options-2-configure-your-etchosts-file)
- [Can I use KSail to manage my existing Kubernetes cluster?](#can-i-use-ksail-to-manage-my-existing-kubernetes-cluster)
- [How do add KSail embedded binaries to my PATH, and alias them?](#how-do-add-ksail-embedded-binaries-to-my-path-and-alias-them)

## How do I configure local DNS?

> [!NOTE] > `KSail` will add support for managing local certificates in the future, but for now this is not supported. You can still solve this yourself via for example [mkcert](https://github.com/FiloSottile/mkcert) to generate and install local certificates for your domain, and then add the `CA` certificate to the Ingress or Gateway resources.

Are you struggling to access your local services hosted in Kubernetes? Do you want to access services via Ingress routes or Gateways, instead of port-forwarding? This is a common issue when using Kubernetes on a local machine, as the services are not accessible without an open host port, and a correctly configured `/etc/hosts` file.

There are a few options to resolve this issue, depending on your setup and preferences. Below are some options to configure local DNS for your Kubernetes Ingress routes and Gateway services.

### Option 1: Use `traefik.me` to resolve `*.traefik.me` to 127.0.0.1

> [!IMPORTANT]
> This options is only applicable if you have a public internet connection and are using `traefik.me` as your top-level domain (TLD).

The easiest option is to use a public DNS service like [traefik.me](https://traefik.me), which allows you to resolve wildcard domains to localhost. To do so all you need to do is to:

1. Ensure your chosen distribution has an open host port to the `LoadBalancer` or `HostPort Service` used by your chosen Ingress controller, or Gateway Controller (e.g. Traefik, Cilium, etc).
2. Configure your Ingress routes or Gateway services to use the `*.traefik.me` domain.

That's it! You can now access your services via `https://<your-service-name>.traefik.me` without needing to configure your `/etc/hosts` file.

### Options 2: Configure your `/etc/hosts` file

If you don't want to use a public DNS service, or you are using a different TLD, you can configure your `/etc/hosts` file to resolve your Ingress routes and Gateway services to localhost. To do so, follow these steps:

1. Ensure your chosen distribution has an open host port to the `LoadBalancer` or `HostPort Service` used by your chosen Ingress controller, or Gateway Controller (e.g. Traefik, Cilium, etc).
2. Open your `/etc/hosts` file in a text editor. You may need to use `sudo` to edit the file.
3. Add the following lines to the file, replacing `<your-service-name>` with the name of your service, and `<your-domain>` with your chosen TLD:

   ```sh
   127.0.0.1 <your-service-name>.<your-domain>
   ```

4. Save the file and exit the text editor.

That's it! You can now access your services via `http://<your-service-name>.<your-domain>`.

## Can I use KSail to manage my existing Kubernetes cluster?

Yes, KSail can be used to interact with an existing Kubernetes cluster, provided it aligns with KSail's supported configurations. KSail is designed to integrate seamlessly with Kubernetes by leveraging standard kubeconfig files and contexts. This means you can use KSail to manage workloads and resources on an existing cluster without requiring a fresh setup. However, commands related to cluster provisioning and lifecycle management—such as `ksail init`, `ksail up`, `ksail down`, `ksail stop`, and `ksail start`—are intended for clusters created through supported providers and may not apply to pre-existing clusters.

To use KSail with an existing cluster, ensure the correct kubeconfig and context are set. You can specify these via the CLI options `--kubeconfig` and `--context`, or by generating a `ksail.yaml` configuration file for your cluster using the `ksail gen config` command.

KSail supports various operations on existing clusters, such as:

- `ksail status` - Check cluster status.
- `ksail update` - Update manifest files.
- `ksail validate` - Validate configurations.
- `ksail connect` - Connect to a cluster to debug issues.
- `ksail gen` - Generate manifests.
- `ksail secrets` - Manage SOPS-encrypted secrets.

## How do add KSail embedded binaries to my PATH, and alias them?

It is possible to add the embedded binaries to your `PATH` and create aliases for them. This allows you to use the binaries without needing to specify the full path each time, and avoid having duplicates of the binaries if you are fine with `ksail` managing the binaries for you.

To do this, you can add the following lines to your shell configuration file (e.g. `~/.bashrc`, `~/.zshrc`, etc.):

```sh
# Path - This will add alle embedded binaries stored in ~/.net/ksail to your PATH
PATH="$PATH:$(find "$HOME/.net/ksail" -mindepth 1 -maxdepth 1 -type d | tr '\n' ':' | sed 's/:$//')"
for dir in $(find "$HOME/.net/ksail" -mindepth 1 -maxdepth 1 -type d); do
  find "$dir" -maxdepth 1 -type f ! -name "*.*" -exec chmod +x {} \;
done

# Aliases - This will create aliases for all embedded binaries stored in ~/.net/ksail, matching your OS and architecture
OS="$(uname | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  darwin) OS="osx" ;;
  linux) OS="linux" ;;
esac
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
esac

for dir in $(find "$HOME/.net/ksail" -mindepth 1 -maxdepth 1 -type d); do
  for file in $(find "$dir" -maxdepth 1 -type f -name "*-${OS}-${ARCH}"); do
    base="$(basename "$file" "-${OS}-${ARCH}")"
    alias "$base"="$file"
  done
done
```
