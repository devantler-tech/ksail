// Typed client for the KSail operator REST API.
//
// Requests are intentionally same-origin (/api/...): the UI container's nginx proxies /api to the
// operator using the API_BASE_URL env var (see web/ui/default.conf.template), and the Helm chart
// ingress routes /api to the operator Service. The SPA therefore needs no build-time base URL.

export interface ClusterStatus {
  phase?: string;
  endpoint?: string;
  nodesReady?: number;
  nodesTotal?: number;
}

export interface ClusterSpec {
  distribution?: string;
  provider?: string;
  gitOpsEngine?: string;
}

export interface Cluster {
  metadata: { name: string; namespace?: string };
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
  // distributions the create form should offer. Absent when the backend relies on the SPA default.
  distributions?: string[];
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

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init);
  if (!response.ok) {
    // Surface the server-provided error body (e.g. the Kubernetes status message) so the UI can
    // show something actionable instead of a bare status code.
    const body = (await response.text()).trim();
    let loginURL: string | undefined;
    try {
      loginURL = (JSON.parse(body) as { loginURL?: string }).loginURL;
    } catch {
      // Body was not JSON; leave loginURL undefined.
    }
    const detail = body === "" ? "" : `: ${body}`;
    throw new ApiError(
      `${init?.method ?? "GET"} ${path}: ${response.status}${detail}`,
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

export function deleteCluster(namespace: string, name: string): Promise<void> {
  return request<void>(`/api/v1/clusters/${namespace}/${name}`, { method: "DELETE" });
}
