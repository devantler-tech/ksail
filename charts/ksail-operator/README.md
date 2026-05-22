# ksail-operator

The KSail Kubernetes operator and optional web UI for declarative cluster management.

The operator reconciles `Cluster` resources (`ksail.io/v1alpha1`) so you can provision and manage KSail-supported distributions from inside a Kubernetes cluster. An optional web UI and OIDC-authenticated REST API are included.

## What it deploys

- **Operator** — a controller that reconciles `Cluster` custom resources.
- **`Cluster` CRD** — installed from the chart's `crds/` directory.
- **RBAC** — a `ServiceAccount` plus the (Cluster)Role and binding the operator needs.
- **REST API** — served by the operator and consumed by the UI (toggle with `api.bindPort`).
- **Web UI** _(optional)_ — a dashboard that talks to the REST API (`ui.enabled`).
- **OIDC auth** _(optional)_ — app-driven OIDC login that protects the REST API and UI (`auth.oidc.enabled`).

## Prerequisites

- Kubernetes 1.27+
- Helm 3.8+

## Installing

The chart is installed from the repository checkout:

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

### Enable the web UI

```sh
helm upgrade --install ksail-operator charts/ksail-operator \
  --namespace ksail-system --create-namespace \
  --set ui.enabled=true
```

Set `ui.readOnly=true` for GitOps-enforced environments so the Git repository stays the single source of truth; the operator enforces read-only server-side. When `ui.ingress.enabled` is `false`, port-forward to reach the UI. The UI Service is named `<release-name>-ksail-operator-ui` (unless you set `fullnameOverride`), so for the install above:

```sh
kubectl port-forward -n ksail-system svc/ksail-operator-ksail-operator-ui 80:80
```

### Enable OIDC authentication

OIDC closes the otherwise-unauthenticated REST API: the API owns the login/callback flow (a confidential client), and the client secret stays server-side. Provide an issuer, client credentials, and a redirect URL (auto-derived from the first ingress host when `ui.ingress` is enabled):

```sh
helm upgrade --install ksail-operator charts/ksail-operator \
  --namespace ksail-system --create-namespace \
  --set ui.enabled=true \
  --set auth.oidc.enabled=true \
  --set auth.oidc.issuerURL=https://dex.example.com \
  --set auth.oidc.clientID=ksail \
  --set-string auth.oidc.clientSecret=CLIENT_SECRET \
  --set auth.oidc.redirectURL=https://ksail.local/api/v1/auth/callback
```

Register the redirect URL with your provider. To keep secrets out of `--set`/values, pre-create a Secret with keys `client-secret` and `session-secret` and reference it via `auth.oidc.existingSecret`.

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
| `operator.image.tag`              | Operator image tag. Defaults to the chart `appVersion` when empty.                                                                   | `""`                                    |
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

### Web UI

| Key                      | Description                                                               | Default                                                       |
|--------------------------|---------------------------------------------------------------------------|---------------------------------------------------------------|
| `ui.enabled`             | Deploy the optional web UI.                                               | `false`                                                       |
| `ui.readOnly`            | Lock the deployment to read-only mode (enforced server-side via the API). | `false`                                                       |
| `ui.image.repository`    | UI image repository.                                                      | `ghcr.io/devantler-tech/ksail/web-ui`                         |
| `ui.image.tag`           | UI image tag. Defaults to the chart `appVersion` when empty.              | `""`                                                          |
| `ui.image.pullPolicy`    | UI image pull policy.                                                     | `IfNotPresent`                                                |
| `ui.replicas`            | Number of UI replicas.                                                    | `1`                                                           |
| `ui.service.type`        | UI Service type.                                                          | `ClusterIP`                                                   |
| `ui.service.port`        | UI Service port.                                                          | `80`                                                          |
| `ui.ingress.enabled`     | Create an Ingress for the UI.                                             | `false`                                                       |
| `ui.ingress.className`   | Ingress class name.                                                       | `""`                                                          |
| `ui.ingress.annotations` | Ingress annotations.                                                      | `{}`                                                          |
| `ui.ingress.hosts`       | Ingress host/path rules.                                                  | `[{host: ksail.local, paths: [{path: /, pathType: Prefix}]}]` |
| `ui.ingress.tls`         | Ingress TLS configuration.                                                | `[]`                                                          |
| `ui.resources`           | UI resource requests/limits.                                              | requests `50m`/`64Mi`, limits `128Mi`                         |

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

| Key                          | Description                   | Default |
|------------------------------|-------------------------------|---------|
| `serviceAccount.create`      | Create a ServiceAccount.      | `true`  |
| `serviceAccount.name`        | ServiceAccount name override. | `""`    |
| `serviceAccount.annotations` | ServiceAccount annotations.   | `{}`    |
| `rbac.create`                | Create RBAC resources.        | `true`  |

### Scheduling

| Key              | Description                | Default |
|------------------|----------------------------|---------|
| `podAnnotations` | Annotations added to pods. | `{}`    |
| `nodeSelector`   | Node selector for pods.    | `{}`    |
| `tolerations`    | Pod tolerations.           | `[]`    |
| `affinity`       | Pod affinity rules.        | `{}`    |

## Learn more

See the [KSail documentation](https://ksail.devantler.tech) for distributions, providers, and GitOps workflows.
