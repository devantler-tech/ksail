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
// hand-written mirror: adding a field or enum value in Go surfaces here after regeneration.
export type ClusterSpec = NonNullable<KSailClusterConfiguration["spec"]["cluster"]>;

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

// ClusterMeta is the static cluster-configuration metadata served by /api/v1/meta: the single
// runtime source of truth for the distribution list, the distribution→provider matrix, and the
// component option lists. The SPA renders its forms from this instead of hard-coding them.
export interface ClusterMeta {
  distributions: string[];
  providers: Record<string, string[]>;
  components: ComponentMeta[];
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

export interface Config {
  readOnly: boolean;
  authEnabled: boolean;
  user?: User;
  // distributions the create form should offer. Absent when the backend relies on the SPA default.
  distributions?: string[];
  // providers reports per-provider availability so the create form can gate options to providers the
  // backend can actually reach. Absent when the backend does not gate (the operator), in which case
  // every provider valid for a distribution is offered.
  providers?: ProviderInfo[];
  // settingsEnabled is true when the backend exposes the credential-settings endpoints (the local UI
  // backend). The operator omits it, so the Settings page stays hidden there.
  settingsEnabled?: boolean;
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
    throw new ApiError(
      `${init?.method ?? "GET"} ${path} (${response.status})${suffix}`,
      response.status,
      loginURL,
    );
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

export function updateCluster(
  namespace: string,
  name: string,
  cluster: Cluster,
): Promise<Cluster> {
  return request<Cluster>(`/api/v1/clusters/${namespace}/${name}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(cluster),
  });
}

export function deleteCluster(namespace: string, name: string): Promise<void> {
  return request<void>(`/api/v1/clusters/${namespace}/${name}`, { method: "DELETE" });
}
