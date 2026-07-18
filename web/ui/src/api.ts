// Typed client for the KSail operator REST API.
//
// Requests are intentionally same-origin (/api/...): in production the operator serves both this
// SPA and the REST API from one origin, so no reverse proxy or build-time base URL is needed. In
// local development, the Vite dev server proxies /api to a locally-running operator.

import type { KSailClusterConfiguration } from "./generated/ksail-config.ts";

export interface Condition {
  type: string;
  status: "True" | "False" | "Unknown";
  reason?: string;
  message?: string;
  lastTransitionTime?: string;
  observedGeneration?: number;
}

export interface SecretReference {
  name: string;
  namespace?: string;
}

export interface ClusterStatus {
  phase?: string;
  endpoint?: string;
  nodesReady?: number;
  nodesTotal?: number;
  conditions?: Condition[];
  kubeconfigSecretRef?: SecretReference;
  lastReconcileTime?: string;
  observedGeneration?: number;
}

// ClusterSpec is the spec.cluster shape, derived from the Go API types via the JSON schema
// (web/ui's `gen:types` script generates ./generated/ksail-config.ts from schemas/). There is no
// hand-written mirror: adding a field or enum value in Go surfaces here after regeneration. Both
// levels are optional in the schema (spec is no longer required), hence the nested NonNullable.
export type ClusterSpec = NonNullable<NonNullable<KSailClusterConfiguration["spec"]>["cluster"]>;

// ComponentKey is the set of spec.cluster fields the UI surfaces as component selectors. The valid
// values and defaults for each come from the /api/v1/meta endpoint, not from this list.
export type ComponentKey =
  | "cni"
  | "csi"
  | "cdi"
  | "metricsServer"
  | "loadBalancer"
  | "certManager"
  | "policyEngine"
  | "gitOpsEngine";

export interface ComponentMeta {
  key: ComponentKey;
  values: string[];
  default: string;
}

// ResourceKindMeta describes one entry of the backend's resource-kind allowlist, served in
// ClusterMeta.resourceKinds and derived from the Go allowlist + predicates (resourceKindTable() and
// friends in pkg/operator/api/resources.go). The SPA builds the workload browser's kind selector and
// action gates from these flags instead of hand-mirroring the Go tables.
export interface ResourceKindMeta {
  kind: string;
  // namespaced is the kind's scope; cluster-scoped kinds list across the whole cluster.
  namespaced: boolean;
  // scalable / restartable / reconcilable gate the scale, rollout-restart, and GitOps-reconcile
  // actions for the kind.
  scalable: boolean;
  restartable: boolean;
  reconcilable: boolean;
  // deletable is false for the cluster-scoped kinds the backend refuses to delete from the workload
  // browser (high blast radius — cluster scope is the deny rule).
  deletable: boolean;
  // browsable is false for the metrics kinds (NodeMetrics/PodMetrics): allowlisted for the Overview's
  // usage monitoring, but deliberately hidden from the kind selector.
  browsable: boolean;
}

// ClusterMeta is the static cluster-configuration metadata served by /api/v1/meta: the single
// runtime source of truth for the distribution list, the distribution→provider matrix, and the
// component option lists. The SPA renders its forms from this instead of hard-coding them.
export interface ClusterMeta {
  distributions: string[];
  providers: Record<string, string[]>;
  components: ComponentMeta[];
  // resourceKinds is the workload browser's kind allowlist with per-kind verb flags. Optional on the
  // wire: absent against an older backend, in which case the SPA falls back to the hand-maintained
  // constants below (RESOURCE_KINDS and friends) — see lib/meta.ts resourceKindLists.
  resourceKinds?: ResourceKindMeta[];
}

export interface ObjectMeta {
  name: string;
  namespace?: string;
  creationTimestamp?: string;
  labels?: Record<string, string>;
}

export interface Cluster {
  metadata: ObjectMeta;
  spec?: { cluster?: ClusterSpec };
  status?: ClusterStatus;
}

export interface ClusterList {
  items?: Cluster[];
}

export interface User {
  subject: string;
  email?: string;
  name?: string;
}

// ProviderInfo reports whether an infrastructure provider is usable on the serving backend, with a
// human-readable reason when it is not.
export interface ProviderInfo {
  name: string;
  available: boolean;
  reason?: string;
}

// Capabilities reports which optional operations the serving backend supports so the SPA can gate
// affordances it cannot fulfill. The local `ksail open web`/desktop backend manages cluster configuration
// via files and reports clusterUpdate=false (the SPA then hides the edit affordance); the operator
// patches the Cluster CR and reports true. New flags are added here as the UI surface grows.
export interface Capabilities {
  clusterUpdate: boolean;
  // workloadRead is true when the backend can read live Kubernetes resources from a target cluster
  // (the read-only workload browser). The SPA shows the Resources view only then.
  workloadRead: boolean;
  // workloadWrite is true when the backend exposes the safe write actions (scale, rollout restart,
  // delete). The SPA combines it with !readOnly before showing the action affordances.
  workloadWrite: boolean;
  // kubeconfigDownload is true when the backend can export a portable kubeconfig for a cluster.
  // The SPA shows the Download-kubeconfig action only then.
  kubeconfigDownload: boolean;
  // applyManifests is true when the backend can server-side-apply raw manifests. The SPA combines it
  // with !readOnly before showing the Apply YAML action.
  applyManifests: boolean;
  // secretsCipher is true when the backend can encrypt/decrypt secrets with SOPS via local age keys
  // (the local backend). The SPA shows the Secrets view only then.
  secretsCipher: boolean;
  // workloadLogs is true when the backend can stream a pod container's logs (the in-browser log
  // viewer). Logs are read-only, so the SPA shows the action without combining it with !readOnly.
  workloadLogs: boolean;
  // workloadExec is true when the backend can exec into a pod container (the in-browser terminal).
  // The SPA combines it with !readOnly before showing the Exec action.
  workloadExec: boolean;
  // clusterStartStop is true when the backend can start/stop an existing cluster's infrastructure
  // without deleting it (the local backend, for Docker clusters). The SPA combines it with !readOnly
  // before showing the start/stop actions; the operator omits it (its clusters are reconciled).
  clusterStartStop: boolean;
  // componentsInstall is true when the backend installs the cluster components (CNI, CSI, …) the
  // create form offers. The SPA gates the create form's component selectors on it so it does not
  // offer options a backend silently drops. The operator installs components; the local backend does
  // not yet, so it reports false and the component selectors are hidden there.
  componentsInstall: boolean;
  // plugins is true when the backend serves web-UI plugins (Headlamp-compatible JS bundles) for the
  // SPA to load. The SPA loads installed plugins and shows the Plugins surface only then. The local
  // `ksail ui`/desktop backend serves plugins from a local directory; the operator leaves it false.
  plugins: boolean;
  // aiChat is true when the backend can run the AI assistant (e.g. GitHub Copilot is configured). The
  // SPA shows the Assistant panel only then; the local backend powers it over Copilot.
  aiChat: boolean;
  // kubeProxy is true when the backend proxies read-only apiserver requests, powering reads beyond the
  // curated allowlist (and Headlamp-compatible plugins' ApiProxy data layer). Local backend only.
  kubeProxy: boolean;
  // pluginInstall is true when the backend can install/uninstall web-UI plugins (PluginInstaller) and
  // is not read-only. The SPA shows the plugin install/uninstall surface only then.
  pluginInstall: boolean;
  // aiChatWrite is true when the assistant may perform write operations — aiChat available AND the
  // deployment is not read-only. The assistant's write tools are gated behind a per-action
  // confirmation card; a read-only deployment rejects them server-side, so the SPA gates the
  // confirmation affordance on this flag.
  aiChatWrite: boolean;
  // pluginCatalog is true when the backend can browse a remote catalog of installable plugins
  // (PluginCatalog) — the local backend searches Artifact Hub for Headlamp plugins. The SPA shows the
  // catalog search box only then; each result installs via the existing install flow.
  pluginCatalog: boolean;
  // kubeWatch is true when the backend streams read-only apiserver watches, powering live incremental
  // updates for the Headlamp-compatible plugins' K8s data layer. When false the SPA falls back to
  // polling. Local backend only.
  kubeWatch: boolean;
  // wsMultiplexer is true when the backend serves the Headlamp WebSocket multiplexer endpoint
  // (/wsMultiplexer), letting a plugin's WebSocketManager multiplex many resource watches over one
  // socket. The plugin K8s data layer prefers it over the per-list SSE watch when advertised, falling
  // back to SSE then polling. Local backend only (rides the same apiserver-watch backing as kubeWatch).
  wsMultiplexer: boolean;
}

// fullCapabilities mirrors the backend's default for a service that does not report capabilities.
// clusterUpdate and componentsInstall default true (the operator — the original backend — supports
// both, so an older backend that omits the flags keeps offering the edit affordance and the create
// form's component selectors rather than hiding working options); the workload + kubeconfig + apply +
// cipher + exec + startStop flags default false because their endpoints may not exist on an older
// backend.
export const fullCapabilities: Capabilities = {
  clusterUpdate: true,
  workloadRead: false,
  workloadWrite: false,
  kubeconfigDownload: false,
  applyManifests: false,
  secretsCipher: false,
  workloadLogs: false,
  workloadExec: false,
  clusterStartStop: false,
  componentsInstall: true,
  // plugins defaults false: an older backend that omits the flag has no plugin endpoints, so the SPA
  // must not attempt to load plugins or show the Plugins surface there.
  plugins: false,
  // aiChat defaults false: an older backend that omits the flag has no chat endpoint, so the SPA must
  // not show the Assistant panel there.
  aiChat: false,
  // kubeProxy defaults false: an older backend that omits the flag has no proxy endpoint.
  kubeProxy: false,
  // pluginInstall defaults false: an older backend that omits the flag has no install endpoint.
  pluginInstall: false,
  // aiChatWrite defaults false: an older backend that omits the flag predates the write-confirm flow,
  // so the SPA keeps the assistant read-only there.
  aiChatWrite: false,
  // pluginCatalog defaults false: an older backend that omits the flag has no catalog endpoint.
  pluginCatalog: false,
  // kubeWatch defaults false: an older backend that omits the flag has no watch endpoint, so the SPA
  // must keep polling rather than open a watch that 404s.
  kubeWatch: false,
  // wsMultiplexer defaults false: an older backend that omits the flag has no /wsMultiplexer endpoint,
  // so the SPA must not attempt a multiplexer connection (it falls back to the SSE watch, then polling).
  wsMultiplexer: false,
};

// logsEventSourceURL builds the same-origin SSE URL for streaming a pod container's logs. EventSource
// is same-origin and GET-only (no headers), which suits the read-only, cookie-authenticated log
// stream — and works in the Wails desktop (SSE passes through the asset server, unlike WebSockets).
export function logsEventSourceURL(
  namespace: string,
  name: string,
  podNamespace: string,
  pod: string,
  container: string,
): string {
  const params = new URLSearchParams({ pod, follow: "true", tail: "1000" });
  if (podNamespace) {
    params.set("namespace", podNamespace);
  }
  if (container) {
    params.set("container", container);
  }

  return `/api/v1/clusters/${namespace}/${name}/logs?${params.toString()}`;
}

export interface Config {
  readOnly: boolean;
  authEnabled: boolean;
  user?: User;
  // capabilities the serving backend supports. Absent only against an older backend; treat absence
  // as the full surface (see fullCapabilities).
  capabilities?: Capabilities;
  // distributions the create form should offer. Absent when the backend relies on the SPA default.
  distributions?: string[];
  // providers reports per-provider availability so the create form can gate options to providers the
  // backend can actually reach. Absent when the backend does not gate (the operator), in which case
  // every provider valid for a distribution is offered.
  providers?: ProviderInfo[];
  // settingsEnabled is true when the backend exposes the credential-settings endpoints (the local UI
  // backend). The operator omits it, so the Settings page stays hidden there.
  settingsEnabled?: boolean;
  // mode labels the serving surface ("operator" | "local") so the SPA can brand itself accurately.
  // Absent against an older backend; the desktop shell (wails:// origin) overrides the label anyway.
  mode?: "operator" | "local";
  // version is the serving backend's build metadata, shown on the About settings screen. Absent on
  // an older backend (and omitted when no metadata was injected, e.g. a plain `go build`).
  version?: VersionInfo;
}

// VersionInfo is the serving backend's build metadata for the About screen.
export interface VersionInfo {
  version?: string;
  commit?: string;
  date?: string;
}

// CredentialSetting describes one provider credential in the Settings page. Secret values are never
// returned (only `stored`/`source`); non-secret values (region, profile, endpoint) carry `value`.
export interface CredentialSetting {
  key: string;
  provider: string;
  label: string;
  envVar: string;
  secret: boolean;
  stored: boolean;
  source: "store" | "env" | "unset";
  value?: string;
}

export interface SettingsResponse {
  credentials: CredentialSetting[];
  // False when no OS secure store (keychain) is reachable: secrets entered here are kept only in
  // memory for the running process and are lost on restart. The page surfaces this so it never
  // implies secrets are securely persisted.
  secureStorageAvailable: boolean;
}

// CredentialUpdate mutates one credential. Omit a field to leave it unchanged; send envVar="" to
// reset to the default variable name, or value="" to clear a stored secret.
export interface CredentialUpdate {
  key: string;
  envVar?: string;
  value?: string;
}

export interface SettingsUpdateRequest {
  updates: CredentialUpdate[];
}

// ApiError carries the HTTP status so callers can react to auth failures (401) specifically.
export class ApiError extends Error {
  readonly status: number;
  readonly loginURL?: string;

  constructor(message: string, status: number, loginURL?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.loginURL = loginURL;
  }
}

// errorMessage extracts a human-readable string from an unknown thrown value. ApiError extends Error,
// so its message (which already carries the server detail) is used directly.
export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

// detailFromBody pulls a human-readable error message out of the server's response body. The API
// returns either {"error": "..."} or {"reason": "..."}; fall back to the raw text otherwise.
function detailFromBody(body: string): { message: string; loginURL?: string } {
  if (body === "") {
    return { message: "" };
  }
  try {
    const parsed = JSON.parse(body) as { error?: string; reason?: string; loginURL?: string };
    return { message: parsed.error ?? parsed.reason ?? body, loginURL: parsed.loginURL };
  } catch {
    return { message: body };
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init);
  if (!response.ok) {
    const body = (await response.text()).trim();
    const { message, loginURL } = detailFromBody(body);
    const suffix = message === "" ? "" : `: ${message}`;
    throw new ApiError(`${init?.method ?? "GET"} ${path} (${response.status})${suffix}`, response.status, loginURL);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return (await response.json()) as T;
}

export function logout(): Promise<void> {
  return request<void>("/api/v1/auth/logout", { method: "POST" });
}

// loginPath is where the app sends the user to start the OIDC flow (handled by the operator API).
export const loginPath = "/api/v1/auth/login";

// eventsPath is the Server-Sent Events stream the SPA subscribes to for live updates (cluster list
// changes today). It is same-origin, so EventSource sends the session cookie automatically.
export const eventsPath = "/api/v1/events";

export function getConfig(): Promise<Config> {
  return request<Config>("/api/v1/config");
}

export function getMeta(): Promise<ClusterMeta> {
  return request<ClusterMeta>("/api/v1/meta");
}

export function getSettings(): Promise<SettingsResponse> {
  return request<SettingsResponse>("/api/v1/settings");
}

export function updateSettings(req: SettingsUpdateRequest): Promise<SettingsResponse> {
  return request<SettingsResponse>("/api/v1/settings", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
}

// ChatSettings is the AI assistant's model selection and reasoning effort. Empty values defer to the
// backend's runtime defaults.
export interface ChatSettings {
  model: string;
  reasoningEffort: string;
}

// AppSettings is the local UI's non-credential preferences (editor command + chat). It is the body
// of both GET and PUT /api/v1/settings/app; a PUT replaces the stored values.
export interface AppSettings {
  editor: string;
  chat: ChatSettings;
}

export function getAppSettings(): Promise<AppSettings> {
  return request<AppSettings>("/api/v1/settings/app");
}

export function updateAppSettings(req: AppSettings): Promise<AppSettings> {
  return request<AppSettings>("/api/v1/settings/app", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
}

// CredentialTestResult is the outcome of a provider connection test. ok=false with a message is a
// normal result (the credentials did not authenticate), not a transport error.
export interface CredentialTestResult {
  provider: string;
  ok: boolean;
  message: string;
}

// testCredential checks whether the stored credentials for a provider authenticate. Only providers
// that support testing (Hetzner, Omni) are accepted; others return a 4xx error.
export function testCredential(provider: string): Promise<CredentialTestResult> {
  return request<CredentialTestResult>(
    `/api/v1/settings/credentials/${encodeURIComponent(provider)}/test`,
    { method: "POST" },
  );
}

export function listClusters(): Promise<ClusterList> {
  return request<ClusterList>("/api/v1/clusters");
}

export function createCluster(cluster: Cluster): Promise<Cluster> {
  return request<Cluster>("/api/v1/clusters", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(cluster),
  });
}

export function updateCluster(namespace: string, name: string, cluster: Cluster): Promise<Cluster> {
  return request<Cluster>(`/api/v1/clusters/${namespace}/${name}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(cluster),
  });
}

export function deleteCluster(namespace: string, name: string): Promise<void> {
  return request<void>(`/api/v1/clusters/${namespace}/${name}`, { method: "DELETE" });
}

// K8sObject is a loose view of an unstructured Kubernetes object: the backend returns each resource
// in its native JSON shape, so only the fields the browser reads are typed; the rest is passthrough.
export interface K8sObject {
  apiVersion?: string;
  kind?: string;
  metadata?: {
    name?: string;
    namespace?: string;
    creationTimestamp?: string;
    [key: string]: unknown;
  };
  status?: Record<string, unknown>;
  [key: string]: unknown;
}

export interface K8sList {
  items?: K8sObject[];
}

// RESOURCE_KINDS is the older-backend fallback for the workload browser's kind selector. Backends
// that serve resourceKinds in /api/v1/meta are the source of truth (derived from the Go allowlist —
// resourceKindTable() in pkg/operator/api/resources.go); this hand-maintained mirror covers backends
// that predate the field and is retained permanently for that reason (see lib/meta.ts
// resourceKindLists). The backend rejects anything outside its own allowlist regardless, so this
// list only drives the kind selector.
export const RESOURCE_KINDS = [
  "Pod",
  "Deployment",
  "StatefulSet",
  "DaemonSet",
  "ReplicaSet",
  "Job",
  "CronJob",
  "Service",
  "Ingress",
  "ConfigMap",
  "PersistentVolumeClaim",
  "Event",
  "Node",
  "Namespace",
  // GitOps CRs (Flux + ArgoCD). Browsable read-only; selecting one on a cluster without that CRD
  // lists with an error (surfaced in the view).
  "Kustomization",
  "HelmRelease",
  "GitRepository",
  "OCIRepository",
  "Application",
] as const;

// listResources fetches resources of a kind from a cluster. resourceNamespace narrows a namespaced
// kind to one namespace; omit it to list across all namespaces.
export function listResources(
  namespace: string,
  name: string,
  kind: string,
  resourceNamespace?: string,
): Promise<K8sList> {
  const params = new URLSearchParams({ kind });
  if (resourceNamespace) {
    params.set("namespace", resourceNamespace);
  }

  return request<K8sList>(`/api/v1/clusters/${namespace}/${name}/resources?${params.toString()}`);
}

// SCALABLE_KINDS / RESTARTABLE_KINDS mirror the backend predicates (ResourceKindScalable /
// ResourceKindRestartable); the backend rejects unsupported kinds regardless.
// RECONCILABLE_KINDS mirrors ResourceKindReconcilable — the GitOps CRs that support a reconcile.
// Like RESOURCE_KINDS, these are permanent older-backend fallbacks: current backends serve the same
// membership via /api/v1/meta's resourceKinds flags (see lib/meta.ts resourceKindLists).
export const RECONCILABLE_KINDS = ["Kustomization", "HelmRelease", "GitRepository", "OCIRepository", "Application"];
export const SCALABLE_KINDS = ["Deployment", "StatefulSet", "ReplicaSet"];
export const RESTARTABLE_KINDS = ["Deployment", "StatefulSet", "DaemonSet"];
// CLUSTER_SCOPED_KINDS are not deletable from the workload browser — the backend rejects a delete of
// these high-blast-radius cluster-scoped kinds, so the SPA hides the Delete affordance for them.
// (Cluster scope IS the backend's deny rule, so the name states the rule.) Fallback like the above:
// current backends carry the same rule as resourceKinds[].deletable === false.
export const CLUSTER_SCOPED_KINDS = ["Node", "Namespace"];

// downloadKubeconfig fetches the cluster's portable kubeconfig and triggers a browser download. It
// streams the bytes into a Blob rather than navigating, so an error response surfaces as an ApiError
// (and a toast) instead of replacing the page.
export async function downloadKubeconfig(namespace: string, name: string): Promise<void> {
  const path = `/api/v1/clusters/${namespace}/${name}/kubeconfig`;
  const response = await fetch(path);
  if (!response.ok) {
    const body = (await response.text()).trim();
    const { message } = detailFromBody(body);
    const suffix = message === "" ? "" : `: ${message}`;
    throw new ApiError(`GET ${path} (${response.status})${suffix}`, response.status);
  }

  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `${name}.kubeconfig`;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

// ApplyResult is the per-document outcome of an apply request.
export interface ApplyResult {
  kind: string;
  name: string;
  namespace?: string;
  status: "applied" | "error";
  error?: string;
}

export interface ApplyResponse {
  results: ApplyResult[];
  dryRun: boolean;
}

// applyManifests server-side-applies multi-document YAML to a cluster. dryRun validates without
// persisting. The body is raw YAML (text/yaml), not JSON.
export function applyManifests(
  namespace: string,
  name: string,
  manifests: string,
  dryRun: boolean,
): Promise<ApplyResponse> {
  const query = dryRun ? "?dryRun=true" : "";

  return request<ApplyResponse>(`/api/v1/clusters/${namespace}/${name}/apply${query}`, {
    method: "POST",
    headers: { "Content-Type": "application/yaml" },
    body: manifests,
  });
}

function resourcePath(
  namespace: string,
  name: string,
  kind: string,
  resourceName: string,
  resourceNamespace: string | undefined,
  suffix: string,
): string {
  const params = new URLSearchParams();
  if (resourceNamespace) {
    params.set("namespace", resourceNamespace);
  }

  const query = params.toString() ? `?${params.toString()}` : "";

  return `/api/v1/clusters/${namespace}/${name}/resources/${kind}/${resourceName}${suffix}${query}`;
}

// ResourceAction identifies a single resource targeted by a mutating action (scale/restart/delete).
// namespace/name are the CLUSTER's; resourceName/resourceNamespace are the resource's own.
export type ResourceAction = {
  namespace: string;
  name: string;
  kind: string;
  resourceName: string;
  resourceNamespace?: string;
};

// mutateResource issues a mutating request against a resource's action endpoint — the shared core of
// scale/restart/delete, so each only differs by its path suffix and request init.
function mutateResource(target: ResourceAction, suffix: string, init: RequestInit): Promise<void> {
  return request<void>(
    resourcePath(target.namespace, target.name, target.kind, target.resourceName, target.resourceNamespace, suffix),
    init,
  );
}

// scaleResource sets the replica count of a scalable workload.
export function scaleResource(target: ResourceAction, replicas: number): Promise<void> {
  return mutateResource(target, "/scale", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ replicas }),
  });
}

// restartResource triggers a rolling restart of a workload.
export function restartResource(target: ResourceAction): Promise<void> {
  return mutateResource(target, "/restart", { method: "POST" });
}

// reconcileResource triggers an immediate GitOps reconcile (Flux/ArgoCD) of a resource.
export function reconcileResource(target: ResourceAction): Promise<void> {
  return mutateResource(target, "/reconcile", { method: "POST" });
}

// deleteResource deletes a resource.
export function deleteResource(target: ResourceAction): Promise<void> {
  return mutateResource(target, "", { method: "DELETE" });
}

// SECRET_FORMATS are the SOPS store formats the cipher view offers.
export const SECRET_FORMATS = ["yaml", "json"];

// cipherRecipients lists the age public keys available locally (for the encrypt recipient selector).
export function cipherRecipients(): Promise<{ recipients: string[] }> {
  return request<{ recipients: string[] }>("/api/v1/secrets/recipients");
}

// encryptSecret SOPS-encrypts plaintext for the given age recipient (empty = the local default).
export function encryptSecret(plaintext: string, recipient: string, format: string): Promise<{ encrypted: string }> {
  return request<{ encrypted: string }>("/api/v1/secrets/encrypt", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ plaintext, recipient, format }),
  });
}

// decryptSecret decrypts a SOPS document using the local age keys.
export function decryptSecret(encrypted: string, format: string): Promise<{ plaintext: string }> {
  return request<{ plaintext: string }>("/api/v1/secrets/decrypt", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ encrypted, format }),
  });
}

// execWebSocketURL builds the same-origin WebSocket URL for an exec session into a pod container.
// (In the Wails desktop, a loopback listener provides a ws:// origin; the browser surfaces are
// same-origin off window.location.)
export function execWebSocketURL(
  namespace: string,
  name: string,
  podNamespace: string,
  pod: string,
  container: string,
): string {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const params = new URLSearchParams({ pod });
  if (podNamespace) {
    params.set("namespace", podNamespace);
  }
  if (container) {
    params.set("container", container);
  }

  return `${protocol}//${window.location.host}/api/v1/clusters/${namespace}/${name}/exec?${params.toString()}`;
}

// PluginInfo describes one installed web-UI plugin the backend serves (see pkg/webui/api/plugins.go).
// The shape mirrors a Headlamp plugin's package.json metadata plus the entry bundle to load. `name`
// is the plugin's addressable id (its install-directory segment), used to build asset URLs; `title`
// is the human-readable display name (falls back to `name`).
export interface PluginInfo {
  name: string;
  title?: string;
  version?: string;
  description?: string;
  homepage?: string;
  // main is the entry bundle, served at /api/v1/plugins/{name}/{main} (defaults to "main.js").
  main: string;
}

// listPlugins fetches the installed web-UI plugins from the backend. Only call it when the backend
// advertises capabilities.plugins; against a backend without the capability the route is unregistered.
export function listPlugins(): Promise<{ plugins: PluginInfo[] }> {
  return request<{ plugins: PluginInfo[] }>("/api/v1/plugins");
}

// pluginAssetURL builds the same-origin URL a plugin's asset (its entry bundle or a sibling file) is
// served from. The plugin loader injects the entry bundle as a classic <script src> pointing here —
// same-origin, so it loads under the strict CSP without 'unsafe-eval' (unlike Headlamp's Function
// loader). The plugin id is URL-encoded; the file path is left intact so a plugin can reference
// sub-path assets (the backend enforces the file stays within the plugin directory).
export function pluginAssetURL(name: string, file: string): string {
  return `/api/v1/plugins/${encodeURIComponent(name)}/${file}`;
}

// PluginCosign carries cosign/sigstore verification material (the strongest install tier). Keyless:
// provide a sigstore `bundle` (inline JSON/base64) plus the expected certificate identity
// (`identitySubject` + `identityIssuer`); the bundle is verified against the public-good trust root.
// Key-based: provide a PEM `publicKey` (a cosign ECDSA key) plus a bundle. When present, a verification
// failure rejects the install (the backend returns 422); see pkg/cli/clusterapi/plugininstall.go.
export interface PluginCosign {
  // bundle is the sigstore bundle JSON, inline (raw or base64-encoded). The SPA supplies it directly —
  // KSail's backend does not fetch it from a URL.
  bundle?: string;
  // publicKey is a PEM-encoded cosign ECDSA public key for key-based verification (keyless otherwise).
  publicKey?: string;
  // identitySubject is the expected keyless signing-certificate SAN; treated as a regex when
  // identitySubjectRegex is set, otherwise matched exactly.
  identitySubject?: string;
  identitySubjectRegex?: boolean;
  // identityIssuer is the expected keyless OIDC issuer; treated as a regex when identityIssuerRegex is
  // set, otherwise matched exactly.
  identityIssuer?: string;
  identityIssuerRegex?: boolean;
}

// PluginInstallRequest installs a Headlamp-format plugin tarball from a URL (POST /api/v1/plugins). The
// authenticity tiers are layered strongest-first: cosign (sigstore bundle, keyless or key-based) is the
// strongest; sha256 (hex, optional) pins the download (integrity); signature (base64 ed25519 detached
// signature over the tarball bytes, optional) authenticates against the backend's trusted public key
// (KSAIL_PLUGIN_SIGNING_PUBKEY) — the backend rejects a claimed signature when no key is configured;
// name (optional) overrides the install id.
export interface PluginInstallRequest {
  url: string;
  sha256?: string;
  signature?: string;
  cosign?: PluginCosign;
  name?: string;
}

// installPlugin installs a plugin from a tarball URL, returning the installed plugin's metadata. Only
// available when capabilities.pluginInstall is true (the local backend, not read-only).
export function installPlugin(req: PluginInstallRequest): Promise<PluginInfo> {
  return request<PluginInfo>("/api/v1/plugins", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
}

// uninstallPlugin removes an installed plugin by id (name).
export function uninstallPlugin(name: string): Promise<void> {
  return request<void>(`/api/v1/plugins/${encodeURIComponent(name)}`, { method: "DELETE" });
}

// CatalogEntry describes one installable plugin from the remote catalog (Artifact Hub Headlamp
// plugins). `url` is a direct .tar.gz the existing install flow consumes, so the Install button hands
// it straight to installPlugin (see pkg/webui/api/plugins.go CatalogEntry).
export interface CatalogEntry {
  name: string;
  description?: string;
  version?: string;
  repository?: string;
  url: string;
  // checksum is the tarball's SHA-256 hex (when the catalog publishes one), forwarded to installPlugin
  // so the backend verifies integrity before extracting.
  checksum?: string;
}

// searchPluginCatalog searches the backend's installable-plugin catalog (the local backend queries
// Artifact Hub for Headlamp plugins). Only call it when capabilities.pluginCatalog is true; against a
// backend without the capability the route is unregistered. An empty query returns the default set.
export function searchPluginCatalog(query: string): Promise<{ entries: CatalogEntry[] }> {
  const params = new URLSearchParams();
  if (query.trim() !== "") {
    params.set("q", query.trim());
  }
  const suffix = params.toString() ? `?${params.toString()}` : "";

  return request<{ entries: CatalogEntry[] }>(`/api/v1/plugins/catalog${suffix}`);
}

// ChatMessage is one prior turn sent as history so the assistant has conversation context.
export interface ChatMessage {
  role: "user" | "assistant";
  content: string;
}

// ChatRequestBody is one chat turn the SPA POSTs: the new message, the prior history, and the active
// cluster context the assistant should reason about.
export interface ChatRequestBody {
  message: string;
  history?: ChatMessage[];
  cluster?: string;
  namespace?: string;
}

// ChatEventType classifies a streamed chat event (mirrors the backend's ChatEventType). "tool-confirm"
// asks the user to approve or deny a write tool before it runs.
export type ChatEventType = "delta" | "tool" | "tool-confirm" | "error" | "done";

// ChatEvent is one streamed event of a chat turn.
export interface ChatEvent {
  type: ChatEventType;
  text?: string;
  // confirmId identifies a pending write-tool confirmation ("tool-confirm" only); echo it back via
  // confirmChatTool to approve or deny the action.
  confirmId?: string;
  // summary is a short, display-only description of the pending write tool ("tool-confirm" only).
  summary?: string;
}

// confirmChatTool resolves a pending write-tool confirmation: it posts the user's decision for the
// action identified by confirmId, which unblocks the in-flight turn on the chat SSE stream so the tool
// proceeds (approved) or is rejected. Returns 204; the outcome streams back on the original turn.
export function confirmChatTool(confirmId: string, approved: boolean): Promise<void> {
  return request<void>("/api/v1/chat/confirm", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ confirmId, approved }),
  });
}

// parseSSEData extracts the concatenated `data:` payload from one SSE frame, or null when the frame
// carries none (e.g. a heartbeat comment). The backend writes "data: <json>" lines (one per data line).
function parseSSEData(frame: string): string | null {
  const data = frame
    .split("\n")
    .filter((line) => line.startsWith("data:"))
    .map((line) => line.slice("data:".length).replace(/^ /, ""))
    .join("\n");

  return data === "" ? null : data;
}

// streamChat POSTs a chat turn and invokes onEvent for each streamed ChatEvent until the turn ends (a
// "done" event) or the stream closes. The response is an SSE stream read from the fetch body; signal
// aborts an in-flight turn. A non-OK response (e.g. 501 when the assistant is unavailable) throws an
// ApiError before streaming starts.
export async function streamChat(
  body: ChatRequestBody,
  onEvent: (event: ChatEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const response = await fetch("/api/v1/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal,
  });

  if (!response.ok) {
    const text = (await response.text()).trim();
    const { message } = detailFromBody(text);
    const suffix = message === "" ? "" : `: ${message}`;
    throw new ApiError(`POST /api/v1/chat (${response.status})${suffix}`, response.status);
  }

  const reader = response.body?.getReader();
  if (!reader) {
    throw new ApiError("chat stream unavailable", 500);
  }

  const decoder = new TextDecoder();
  let buffer = "";

  for (;;) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }

    buffer += decoder.decode(value, { stream: true });

    // SSE frames are separated by a blank line; process every complete frame in the buffer.
    let separator = buffer.indexOf("\n\n");
    while (separator !== -1) {
      const frame = buffer.slice(0, separator);
      buffer = buffer.slice(separator + 2);

      const data = parseSSEData(frame);
      if (data !== null) {
        try {
          onEvent(JSON.parse(data) as ChatEvent);
        } catch {
          // Ignore a malformed frame rather than aborting the whole turn.
        }
      }

      separator = buffer.indexOf("\n\n");
    }
  }
}
