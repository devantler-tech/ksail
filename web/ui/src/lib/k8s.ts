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

// HOST_CLUSTER_LABEL marks the Cluster resource the operator self-registers for the cluster it
// runs ON (the hub). The UI badges it and hides the lifecycle actions (edit/delete), which the API
// rejects server-side anyway — the hub hosting the operator is not the operator's to destroy.
export const HOST_CLUSTER_LABEL = "ksail.io/host-cluster";

export function isHostCluster(cluster: Cluster): boolean {
  return cluster.metadata.labels?.[HOST_CLUSTER_LABEL] === "true";
}

// CLUSTER_PHASE_STOPPED is the display-only phase the SPA derives for a cluster the backend reports as
// stopped (its infrastructure exists but is not running). The backend does not emit it as a phase
// value — that would be a breaking API enum change — it leaves status.phase unset and attaches a
// Ready=False/reason=Stopped condition; clusterPhase folds that back into this presentational phase so
// StatusBadge renders "Stopped" instead of a falsely green "Ready" or a bare "Unknown".
export const CLUSTER_PHASE_STOPPED = "Stopped";

// clusterPhase returns the phase to display for a cluster: its reported status.phase when set,
// otherwise CLUSTER_PHASE_STOPPED when the backend signalled a stopped cluster via a
// Ready=False/reason=Stopped condition, otherwise "" (which StatusBadge renders as Unknown). This is
// the single place the stopped-condition convention is decoded, so every status surface agrees.
export function clusterPhase(cluster: Cluster): string {
  const phase = cluster.status?.phase ?? "";
  if (phase !== "") {
    return phase;
  }

  const stopped = (cluster.status?.conditions ?? []).some(
    (condition) => condition.type === "Ready" && condition.status === "False" && condition.reason === "Stopped",
  );

  return stopped ? CLUSTER_PHASE_STOPPED : "";
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

// RecentEventsOptions tunes recentEvents for each call site: restrict to a Warning/Normal type,
// keep only events satisfying a predicate (e.g. targeting a resource, or matching a free-text
// search), and/or cap the result. All are optional; an unset filter keeps every event.
export interface RecentEventsOptions {
  type?: EventFields["type"];
  matches?: (event: EventFields) => boolean;
  limit?: number;
}

// recentEvents normalizes a list of raw Event objects, applies the optional type/predicate filters,
// sorts newest-first by last-seen time, and caps to `limit` when given. Centralizes the
// map(eventFields)→filter→sort-desc(→slice) pipeline shared by the Overview, Resources, and Events
// views so the normalize/sort idiom lives in one place.
export function recentEvents(objects: K8sObject[], options: RecentEventsOptions = {}): EventFields[] {
  const { type, matches, limit } = options;

  const events = objects
    .map(eventFields)
    .filter((event) => (type === undefined || event.type === type) && (matches === undefined || matches(event)))
    .sort((a, b) => eventLastSeenMs(b) - eventLastSeenMs(a));

  return limit === undefined ? events : events.slice(0, limit);
}

// nodeConditions returns a Node's status conditions as loose records.
function nodeConditions(node: K8sObject): Record<string, unknown>[] {
  const status = record(node.status);
  const conditions = Array.isArray(status?.conditions) ? status.conditions : [];

  return conditions.map(record).filter((cond) => cond !== undefined);
}

// nodeReady reports whether a Node's Ready condition is True.
export function nodeReady(node: K8sObject): boolean {
  return nodeConditions(node).some((cond) => cond.type === "Ready" && cond.status === "True");
}

// NodeProblem is a Node condition signalling trouble (e.g. MemoryPressure/DiskPressure True), kept
// with its node so the Overview can synthesize health conditions from live state.
export interface NodeProblem {
  node: string;
  type: string;
  message: string;
  lastTransitionTime?: string;
}

// nodeProblems returns the Node's abnormal conditions: Ready is a problem when not True, every other
// condition (the kubelet's pressure/availability signals) is a problem when True.
export function nodeProblems(node: K8sObject): NodeProblem[] {
  const name = str(node.metadata?.name);

  return nodeConditions(node)
    .filter((cond) => (cond.type === "Ready" ? cond.status !== "True" : cond.status === "True"))
    .map((cond) => ({
      node: name,
      type: str(cond.type),
      message: str(cond.message) || str(cond.reason),
      lastTransitionTime: str(cond.lastTransitionTime) || undefined,
    }));
}

// nodeIsControlPlane reports whether a Node carries a control-plane role label (including the legacy
// master label Talos/older clusters still use).
export function nodeIsControlPlane(node: K8sObject): boolean {
  const labels = node.metadata?.labels;
  const map = record(labels) ?? {};

  return "node-role.kubernetes.io/control-plane" in map || "node-role.kubernetes.io/master" in map;
}

// nodeSystemInfo extracts the kubelet version and OS image from a Node's status.nodeInfo ("" when
// absent), surfacing the cluster's actual Kubernetes version and OS in the Overview.
export function nodeSystemInfo(node: K8sObject): { kubeletVersion: string; osImage: string } {
  const info = record(record(node.status)?.nodeInfo);

  return { kubeletVersion: str(info?.kubeletVersion), osImage: str(info?.osImage) };
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
