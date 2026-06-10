// Resource-usage model for the Overview's monitoring section: combines Nodes (capacity/allocatable),
// Pods (scheduled requests/limits), and the metrics API (live usage, when a metrics-server is
// installed) into per-node and cluster-wide totals.

import type { K8sObject } from "../api.ts";
import { nodeIsControlPlane, nodeReady, podPhase } from "./k8s.ts";
import { parseQuantity } from "./quantity.ts";

// ResourceTotals is one resource dimension (CPU in cores or memory in bytes) for a node or the
// cluster: what exists, what scheduled pods ask for, and live usage when the metrics API serves it.
export interface ResourceTotals {
  capacity: number;
  allocatable: number;
  requests: number;
  limits: number;
  usage?: number;
}

export interface NodeUsage {
  name: string;
  controlPlane: boolean;
  ready: boolean;
  cpu: ResourceTotals;
  memory: ResourceTotals;
  pods: { allocatable: number; count: number };
}

export interface ClusterUsage {
  nodes: NodeUsage[];
  cpu: ResourceTotals;
  memory: ResourceTotals;
  pods: { allocatable: number; count: number };
  // metricsAvailable is true when the metrics API returned usage for at least one node; the UI
  // otherwise falls back to requests-only visuals and explains why.
  metricsAvailable: boolean;
}

// PodConsumption is one pod's live usage from the metrics API, for the "top consumers" lists.
export interface PodConsumption {
  name: string;
  namespace: string;
  cpu: number;
  memory: number;
}

// asRecord narrows unstructured JSON to an object map (usage.ts reads metrics/pod shapes the loose
// K8sObject type does not model).
function asRecord(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : undefined;
}

// cpuMemory reads the cpu/memory quantities off an unstructured resource map (a node's
// capacity/allocatable, a container's requests/limits, or a metrics usage block).
function cpuMemory(value: unknown): { cpu: number; memory: number } {
  const map = asRecord(value);

  return {
    cpu: parseQuantity(map?.cpu) ?? 0,
    memory: parseQuantity(map?.memory) ?? 0,
  };
}

// containersOf returns the containers array of a pod spec or pod-metrics object.
function containersOf(value: unknown): Record<string, unknown>[] {
  const containers = asRecord(value)?.containers;
  if (!Array.isArray(containers)) {
    return [];
  }

  return containers.map(asRecord).filter((container) => container !== undefined);
}

// sumPodResources accumulates one pod's per-container requests and limits into the per-node bucket.
function sumPodResources(pod: K8sObject, bucket: { cpu: ResourceTotals; memory: ResourceTotals }) {
  for (const container of containersOf(pod.spec)) {
    const resources = asRecord(container.resources);
    const requests = cpuMemory(resources?.requests);
    const limits = cpuMemory(resources?.limits);

    bucket.cpu.requests += requests.cpu;
    bucket.cpu.limits += limits.cpu;
    bucket.memory.requests += requests.memory;
    bucket.memory.limits += limits.memory;
  }
}

function emptyTotals(): ResourceTotals {
  return { capacity: 0, allocatable: 0, requests: 0, limits: 0 };
}

// podIsScheduledActive reports whether a pod currently occupies node resources: it is bound to a
// node and not in a terminal phase (matching how `kubectl describe node` counts allocated resources).
function podIsScheduledActive(pod: K8sObject): boolean {
  const phase = podPhase(pod);

  return asRecord(pod.spec)?.nodeName !== undefined && phase !== "Succeeded" && phase !== "Failed";
}

// buildClusterUsage assembles the per-node and cluster-wide usage model. nodeMetrics may be empty
// (no metrics-server): live usage is then omitted and metricsAvailable is false.
export function buildClusterUsage(
  nodes: K8sObject[],
  pods: K8sObject[],
  nodeMetrics: K8sObject[],
): ClusterUsage {
  const usageByNode = new Map<string, { cpu: number; memory: number }>();
  for (const metric of nodeMetrics) {
    const name = metric.metadata?.name;
    if (typeof name === "string") {
      usageByNode.set(name, cpuMemory(metric.usage));
    }
  }

  const byNode = new Map<string, NodeUsage>();
  for (const node of nodes) {
    const name = node.metadata?.name;
    if (typeof name !== "string") {
      continue;
    }

    const status = asRecord(node.status);
    const capacity = cpuMemory(status?.capacity);
    const allocatable = cpuMemory(status?.allocatable);
    const usage = usageByNode.get(name);

    byNode.set(name, {
      name,
      controlPlane: nodeIsControlPlane(node),
      ready: nodeReady(node),
      cpu: { ...emptyTotals(), capacity: capacity.cpu, allocatable: allocatable.cpu, usage: usage?.cpu },
      memory: {
        ...emptyTotals(),
        capacity: capacity.memory,
        allocatable: allocatable.memory,
        usage: usage?.memory,
      },
      pods: { allocatable: parseQuantity(asRecord(status?.allocatable)?.pods) ?? 0, count: 0 },
    });
  }

  for (const pod of pods) {
    if (!podIsScheduledActive(pod)) {
      continue;
    }

    const entry = byNode.get(String(asRecord(pod.spec)?.nodeName));
    if (!entry) {
      continue;
    }

    entry.pods.count += 1;
    sumPodResources(pod, entry);
  }

  // Control planes first, then workers, both alphabetically — the same order an operator scans.
  const nodeUsages = [...byNode.values()].sort(
    (a, b) => Number(b.controlPlane) - Number(a.controlPlane) || a.name.localeCompare(b.name),
  );

  const cluster: ClusterUsage = {
    nodes: nodeUsages,
    cpu: emptyTotals(),
    memory: emptyTotals(),
    pods: { allocatable: 0, count: 0 },
    metricsAvailable: usageByNode.size > 0,
  };

  for (const node of nodeUsages) {
    for (const key of ["cpu", "memory"] as const) {
      cluster[key].capacity += node[key].capacity;
      cluster[key].allocatable += node[key].allocatable;
      cluster[key].requests += node[key].requests;
      cluster[key].limits += node[key].limits;
      if (node[key].usage !== undefined) {
        cluster[key].usage = (cluster[key].usage ?? 0) + node[key].usage;
      }
    }

    cluster.pods.allocatable += node.pods.allocatable;
    cluster.pods.count += node.pods.count;
  }

  return cluster;
}

// buildPodConsumption maps pod metrics to per-pod live usage, summed across containers.
export function buildPodConsumption(podMetrics: K8sObject[]): PodConsumption[] {
  return podMetrics.map((metric) => {
    const totals = { cpu: 0, memory: 0 };
    for (const container of containersOf(metric)) {
      const usage = cpuMemory(container.usage);
      totals.cpu += usage.cpu;
      totals.memory += usage.memory;
    }

    return {
      name: metric.metadata?.name ?? "",
      namespace: metric.metadata?.namespace ?? "",
      ...totals,
    };
  });
}

// topConsumers returns the count highest-usage pods for a dimension, dropping zero-usage entries.
export function topConsumers(
  pods: PodConsumption[],
  dimension: "cpu" | "memory",
  count: number,
): PodConsumption[] {
  return pods
    .filter((pod) => pod[dimension] > 0)
    .sort((a, b) => b[dimension] - a[dimension])
    .slice(0, count);
}
