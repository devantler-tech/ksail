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

## How do I configure local DNS?

> [!NOTE]
> `KSail` will add support for managing local certificates in the future, but for now this is not supported. You can still solve this yourself via for example [mkcert](https://github.com/FiloSottile/mkcert) to generate and install local certificates for your domain, and then add the `CA` certificate to the Ingress or Waypoint resources.

Are you struggling to access your local services hosted in Kubernetes? Do you want to access services via Ingress routes or Waypoints, instead of port-forwarding? This is a common issue when using Kubernetes on a local machine, as the services are not accessible without an open host port, and a correctly configured `/etc/hosts` file.

There are a few options to resolve this issue, depending on your setup and preferences. Below are some options to configure local DNS for your Kubernetes Ingress routes and Waypoint services.

### Option 1: Use `traefik.me` to resolve `*.traefik.me` to 127.0.0.1

> [!IMPORTANT]
> This options is only applicable if you have a public internet connection and are using `traefik.me` as your top-level domain (TLD).

The easiest option is to use a public DNS service like [traefik.me](https://traefik.me), which allows you to resolve wildcard domains to localhost. To do so all you need to do is to:

1. Ensure your chosen distribution has an open host port to the `LoadBalancer` or `HostPort Service` used by your chosen Ingress controller, or Waypoint Controller (e.g. Traefik, Cilium, etc).
2. Configure your Ingress routes or Waypoint services to use the `*.traefik.me` domain.

That's it! You can now access your services via `https://<your-service-name>.traefik.me` without needing to configure your `/etc/hosts` file.

### Options 2: Configure your `/etc/hosts` file

If you don't want to use a public DNS service, or you are using a different TLD, you can configure your `/etc/hosts` file to resolve your Ingress routes and Waypoint services to localhost. To do so, follow these steps:

1. Ensure your chosen distribution has an open host port to the `LoadBalancer` or `HostPort Service` used by your chosen Ingress controller, or Waypoint Controller (e.g. Traefik, Cilium, etc).
2. Open your `/etc/hosts` file in a text editor. You may need to use `sudo` to edit the file.
3. Add the following lines to the file, replacing `<your-service-name>` with the name of your service, and `<your-domain>` with your chosen TLD:

   ```sh
   127.0.0.1 <your-service-name>.<your-domain>
   ```

4. Save the file and exit the text editor.

That's it! You can now access your services via `http://<your-service-name>.<your-domain>`.
