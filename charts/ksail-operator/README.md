# ksail-operator

The KSail Kubernetes operator and embedded web UI for declarative cluster management.

The operator reconciles `Cluster` resources (`ksail.io/v1alpha1`) so you can provision and manage KSail-supported distributions from inside a Kubernetes cluster. A web UI and OIDC-authenticated REST API are embedded in the operator binary.

## What it deploys

- **Operator** — a controller that reconciles `Cluster` custom resources.
- **`Cluster` CRD** — installed from the chart's `crds/` directory.
- **RBAC** — a `ServiceAccount` plus the (Cluster)Role and binding the operator needs.
- **REST API & Web UI** — both embedded in the operator binary and served on the same port (`api.bindPort`; set to `0` to disable both). There is no separate UI container, Deployment, or Service.
- **OIDC auth** _(optional)_ — app-driven OIDC login that protects the REST API and UI (`auth.oidc.enabled`).
- **Host cluster registration** — the operator self-registers the cluster it runs on as a `Cluster` resource named `host` (labelled `ksail.io/host-cluster`) in the release namespace, so the hub itself appears in the cluster list and its workloads can be browsed in the UI — like Rancher's `local` cluster or Argo CD's `in-cluster` destination. The operator never provisions, updates, or deletes the underlying cluster for this entry, and the API rejects lifecycle mutations on it. Disable with `hostCluster.enabled=false`.

> **Note:** The REST API is unauthenticated by default. Enable OIDC (`auth.oidc.enabled=true`) to require sign-in, or set `api.bindPort=0` to disable the API entirely when you don't need the UI.

## Prerequisites

- Kubernetes 1.27+
- Helm 3.8+

## Installing

Each KSail release publishes the chart as an OCI artifact to GHCR. Install a pinned version (replace `<version>` with a [release](https://github.com/devantler-tech/ksail/releases) version, e.g. `7.35.0`):

```sh
helm install ksail-operator oci://ghcr.io/devantler-tech/charts/ksail-operator \
  --version <version> --namespace ksail-system --create-namespace
```

Or install from the repository checkout:

```sh
helm install ksail-operator charts/ksail-operator --namespace ksail-system --create-namespace
```

Helm installs the bundled `Cluster` CRD automatically. Override any value with `--set key=value` or a `-f values.yaml` file (see [Configuration](#configuration)).

## Uninstalling

```sh
helm uninstall ksail-operator --namespace ksail-system
```

Helm does **not** remove CRDs installed from `crds/`. Delete the `Cluster` CRD manually if you no longer need it (this also deletes all `Cluster` resources):

```sh
kubectl delete crd clusters.ksail.io
```

## Usage

### Create a cluster

With the operator running, apply a `Cluster` resource:

```sh
kubectl apply -f - <<'EOF'
apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: my-cluster
  namespace: ksail-system
spec:
  cluster:
    distribution: VCluster
EOF

kubectl get clusters -n ksail-system -w
```

### Reach the web UI

The web UI is embedded in the operator binary and served by the operator itself on the API port (`api.bindPort`, default `8080`) — same origin as the REST API. It is available whenever the API is enabled; no extra `--set` flags are needed.

Set `ui.readOnly=true` for GitOps-enforced environments so the Git repository stays the single source of truth; the operator enforces read-only server-side. When `ui.ingress.enabled` is `false`, port-forward the operator Service to reach the UI. The Service is named `<release-name>-ksail-operator` (unless you set `fullnameOverride`), so for the install above:

```sh
kubectl port-forward -n ksail-system svc/ksail-operator-ksail-operator 8080:8080
```

Then open <http://localhost:8080>.

### Enable OIDC authentication

OIDC closes the otherwise-unauthenticated REST API: the API owns the login/callback flow (a confidential client), and the client secret stays server-side. The provider must be able to reach the API's callback at a stable URL, so this example exposes it through an Ingress at `ksail.local` (with the Ingress enabled, `redirectURL` would otherwise auto-derive from the first host):

```sh
helm upgrade --install ksail-operator charts/ksail-operator \
  --namespace ksail-system --create-namespace \
  --set ui.ingress.enabled=true \
  --set ui.ingress.hosts[0].host=ksail.local \
  --set auth.oidc.enabled=true \
  --set auth.oidc.issuerURL=https://dex.example.com \
  --set auth.oidc.clientID=ksail \
  --set-string auth.oidc.clientSecret=CLIENT_SECRET \
  --set auth.oidc.redirectURL=https://ksail.local/api/v1/auth/callback
```

Register the redirect URL with your provider, and point `ksail.local` at your Ingress controller (terminating TLS for the `https` callback). To keep secrets out of `--set`/values, pre-create a Secret with keys `client-secret` and `session-secret` and reference it via `auth.oidc.existingSecret`.

## Configuration

### Common

| Key                | Description                                          | Default |
|--------------------|------------------------------------------------------|---------|
| `nameOverride`     | Override the chart name in generated resource names. | `""`    |
| `fullnameOverride` | Override the fully qualified resource name.          | `""`    |

### Operator

| Key                               | Description                                                                                                                          | Default                                 |
|-----------------------------------|--------------------------------------------------------------------------------------------------------------------------------------|-----------------------------------------|
| `operator.enabled`                | Deploy the operator.                                                                                                                 | `true`                                  |
| `operator.image.repository`       | Operator image repository.                                                                                                           | `ghcr.io/devantler-tech/ksail`          |
| `operator.image.tag`              | Operator image tag. Defaults to `v<appVersion>` (the `v`-prefixed chart `appVersion`, matching the released image tag) when empty.   | `""`                                    |
| `operator.image.pullPolicy`       | Operator image pull policy.                                                                                                          | `IfNotPresent`                          |
| `operator.replicas`               | Number of operator replicas.                                                                                                         | `1`                                     |
| `operator.leaderElection`         | Ensure a single active operator across replicas.                                                                                     | `true`                                  |
| `operator.dockerSocket.enabled`   | Mount the host Docker socket for Docker-based distributions (Kind/K3d). Privileged and single-node — prefer VCluster/K3s in-cluster. | `false`                                 |
| `operator.metricsBindAddress`     | Metrics bind address (`"0"` disables).                                                                                               | `"0"`                                   |
| `operator.healthProbeBindAddress` | Health probe bind address.                                                                                                           | `":8081"`                               |
| `operator.resources`              | Operator resource requests/limits.                                                                                                   | requests `100m`/`128Mi`, limits `256Mi` |

### API

| Key            | Description                                                                                | Default |
|----------------|--------------------------------------------------------------------------------------------|---------|
| `api.bindPort` | Port the operator REST API listens on (consumed by the UI). Set to `0` to disable the API. | `8080`  |

### Host cluster

| Key                   | Description                                                                                                            | Default |
|-----------------------|------------------------------------------------------------------------------------------------------------------------|---------|
| `hostCluster.enabled` | Self-register the cluster the operator runs on as a `Cluster` resource named `host` so it appears in the cluster list. | `true`  |

### Web UI

The UI is embedded in the operator binary and served on `api.bindPort` — it has no image, replica, Service, or resource settings of its own.

| Key                      | Description                                                               | Default                                                       |
|--------------------------|---------------------------------------------------------------------------|---------------------------------------------------------------|
| `ui.readOnly`            | Lock the deployment to read-only mode (enforced server-side via the API). | `false`                                                       |
| `ui.ingress.enabled`     | Create an Ingress for the UI.                                             | `false`                                                       |
| `ui.ingress.className`   | Ingress class name.                                                       | `""`                                                          |
| `ui.ingress.annotations` | Ingress annotations.                                                      | `{}`                                                          |
| `ui.ingress.hosts`       | Ingress host/path rules.                                                  | `[{host: ksail.local, paths: [{path: /, pathType: Prefix}]}]` |
| `ui.ingress.tls`         | Ingress TLS configuration.                                                | `[]`                                                          |

### OIDC authentication

| Key                        | Description                                                                                                         | Default                  |
|----------------------------|---------------------------------------------------------------------------------------------------------------------|--------------------------|
| `auth.oidc.enabled`        | Enable OIDC authentication for the REST API and UI.                                                                 | `false`                  |
| `auth.oidc.issuerURL`      | OIDC issuer (discovery) URL, e.g. `https://dex.example.com`.                                                        | `""`                     |
| `auth.oidc.clientID`       | OAuth client ID.                                                                                                    | `""`                     |
| `auth.oidc.clientSecret`   | OAuth client secret (sensitive; rendered into a Secret).                                                            | `""`                     |
| `auth.oidc.sessionSecret`  | Session cookie signing secret. Auto-generated and preserved across upgrades when empty.                             | `""`                     |
| `auth.oidc.existingSecret` | Reference a pre-created Secret with keys `client-secret` and `session-secret`.                                      | `""`                     |
| `auth.oidc.scopes`         | Scopes requested from the issuer (`openid` is always included).                                                     | `"openid email profile"` |
| `auth.oidc.redirectURL`    | OIDC callback URL. Defaults to `<scheme>://<first ingress host>/api/v1/auth/callback` when `ui.ingress` is enabled. | `""`                     |

### ServiceAccount & RBAC

| Key                          | Description                                                                                                                                                                         | Default |
|------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|---------|
| `serviceAccount.create`      | Create a ServiceAccount.                                                                                                                                                            | `true`  |
| `serviceAccount.name`        | ServiceAccount name override.                                                                                                                                                       | `""`    |
| `serviceAccount.annotations` | ServiceAccount annotations.                                                                                                                                                         | `{}`    |
| `rbac.create`                | Create RBAC resources.                                                                                                                                                              | `true`  |
| `rbac.clusterAdmin`          | Grant a full wildcard ClusterRole (cluster-admin) instead of the least-privilege default. Opt in only when a distribution/component set needs resources the default does not cover. | `false` |

### Image pull secrets

| Key                | Description                                                                                                                          | Default |
|--------------------|--------------------------------------------------------------------------------------------------------------------------------------|---------|
| `imagePullSecrets` | Existing `kubernetes.io/dockerconfigjson` Secrets used to pull the operator image. Applied to the Deployment and the ServiceAccount. | `[]`    |

The published operator image is public, so the default is empty and pulls fall back to the node's
registry credentials. Set this when the image lives in a private registry — or when you would rather
the pull not depend on node-level auth at all, since a stale node credential leaves new pods unable to
pull and silently blocks upgrades:

```yaml
imagePullSecrets:
  - name: ghcr-auth
```

The secrets are attached to the operator Deployment's pod spec **and** to the chart's ServiceAccount,
so pods that run under that ServiceAccount inherit them.

### Scheduling

| Key              | Description                | Default |
|------------------|----------------------------|---------|
| `podAnnotations` | Annotations added to pods. | `{}`    |
| `nodeSelector`   | Node selector for pods.    | `{}`    |
| `tolerations`    | Pod tolerations.           | `[]`    |
| `affinity`       | Pod affinity rules.        | `{}`    |

## Learn more

See the [KSail documentation](https://ksail.devantler.tech) for distributions, providers, and GitOps workflows.
