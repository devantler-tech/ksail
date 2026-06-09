import type { Cluster, K8sObject } from "../api.ts";
import { epochMs } from "./format.ts";

// clusterKey is the "namespace/name" identity used to address a cluster across the SPA (matches the
// key App.tsx selects on). splitClusterKey is its inverse; both are DNS labels, so name has no "/".
export function clusterKey(cluster: Cluster): string {
  return `${cluster.metadata.namespace ?? "default"}/${cluster.metadata.name}`;
}

export function splitClusterKey(key: string): [string, string] {
  const slash = key.indexOf("/");

  return [key.slice(0, slash), key.slice(slash + 1)];
}

// str safely reads a string field from an unstructured value (the backend returns native Kubernetes
// JSON, typed loosely as K8sObject), returning "" for anything that is not a string.
function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

// record narrows an unknown to an object map for nested field access, or undefined.
function record(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : undefined;
}

// EventFields is the normalized view of a Kubernetes Event the SPA renders. Events are listed via the
// core/v1 Events allowlist entry, but fields are read defensively so an events.k8s.io/v1 shape
// (note/regarding/series) still surfaces.
export interface EventFields {
  type: string;
  reason: string;
  message: string;
  objectKind: string;
  objectName: string;
  count: number;
  lastSeen?: string;
}

// eventFields normalizes an Event object. lastSeen prefers the most recent timestamp the object
// carries (lastTimestamp / eventTime / series.lastObservedTime), falling back to creationTimestamp.
export function eventFields(obj: K8sObject): EventFields {
  const involved = record(obj.involvedObject) ?? record(obj.regarding);
  const series = record(obj.series);
  const countValue = obj.count ?? series?.count;

  return {
    type: str(obj.type) || "Normal",
    reason: str(obj.reason),
    message: str(obj.message) || str(obj.note),
    objectKind: str(involved?.kind),
    objectName: str(involved?.name),
    count: typeof countValue === "number" && countValue > 0 ? countValue : 1,
    lastSeen:
      str(obj.lastTimestamp) ||
      str(obj.eventTime) ||
      str(series?.lastObservedTime) ||
      str(obj.firstTimestamp) ||
      str(obj.metadata?.creationTimestamp) ||
      undefined,
  };
}

// eventLastSeenMs returns the event's last-seen time in epoch ms for sorting (0 when unknown).
export function eventLastSeenMs(fields: EventFields): number {
  return epochMs(fields.lastSeen);
}

// nodeReady reports whether a Node's Ready condition is True.
export function nodeReady(node: K8sObject): boolean {
  const status = record(node.status);
  const conditions = Array.isArray(status?.conditions) ? status.conditions : [];
  for (const condition of conditions) {
    const cond = record(condition);
    if (cond && cond.type === "Ready") {
      return cond.status === "True";
    }
  }

  return false;
}

// podPhase returns a Pod's status.phase (e.g. "Running", "Pending"), or "Unknown" when absent.
export function podPhase(pod: K8sObject): string {
  const status = record(pod.status);

  return str(status?.phase) || "Unknown";
}

// podReady reports whether a Running Pod has all of its containers ready (a Pod can be Running while
// a container is crash-looping). Non-Running pods are not "ready".
export function podReady(pod: K8sObject): boolean {
  if (podPhase(pod) !== "Running") {
    return false;
  }

  const status = record(pod.status);
  const statuses = Array.isArray(status?.containerStatuses) ? status.containerStatuses : [];
  if (statuses.length === 0) {
    return false;
  }

  return statuses.every((entry) => record(entry)?.ready === true);
}
