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

export interface Config {
  readOnly: boolean;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init);
  if (!response.ok) {
    // Surface the server-provided error body (e.g. the Kubernetes status message) so the UI can
    // show something actionable instead of a bare status code.
    const body = (await response.text()).trim();
    const detail = body === "" ? "" : `: ${body}`;
    throw new Error(`${init?.method ?? "GET"} ${path}: ${response.status}${detail}`);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return (await response.json()) as T;
}

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
