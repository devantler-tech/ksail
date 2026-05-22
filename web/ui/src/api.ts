// Typed client for the KSail operator REST API.
//
// Requests are intentionally same-origin (/api/...): in production the operator serves both this
// SPA and the REST API from one origin, so no reverse proxy or build-time base URL is needed. In
// local development, the Vite dev server proxies /api to a locally-running operator.

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

export interface ClusterSpec {
  distribution?: string;
  provider?: string;
  controlPlanes?: number;
  workers?: number;
  cni?: string;
  csi?: string;
  cdi?: string;
  metricsServer?: string;
  loadBalancer?: string;
  certManager?: string;
  policyEngine?: string;
  gitOpsEngine?: string;
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

export interface Config {
  readOnly: boolean;
  authEnabled: boolean;
  user?: User;
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
