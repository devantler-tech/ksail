// Pure helpers for the workload browser (ResourcesView and its panels): reading loosely-typed fields
// off unstructured K8sObjects, deriving a row's status, sorting, serializing manifests, and building
// the target a write action addresses. Kept in lib so the views stay presentational and the
// derivations are unit-testable without rendering.

import yaml from "js-yaml";
import type { K8sObject, ResourceAction } from "../api.ts";
import { epochMs } from "./format.ts";
import { splitClusterKey } from "./k8s.ts";

export type SortKey = "name" | "namespace" | "status" | "age";

// objectKey is a stable React key / sort tiebreak for a list row ("namespace/name", index fallback).
export function objectKey(obj: K8sObject, index: number): string {
  const meta = obj.metadata;

  return `${meta?.namespace ?? ""}/${meta?.name ?? index}`;
}

// currentReplicas reads spec.replicas from an unstructured object, defaulting to 0.
export function currentReplicas(obj: K8sObject): number {
  const spec = obj.spec as { replicas?: number } | undefined;

  return spec?.replicas ?? 0;
}

// podContainers reads spec.containers[].name from a Pod object (for the logs/exec container picker).
export function podContainers(obj: K8sObject): string[] {
  const spec = obj.spec as { containers?: { name?: string }[] } | undefined;

  return (spec?.containers ?? []).map((container) => container.name ?? "").filter((name) => name !== "");
}

// resourceStatus derives an at-a-glance health/reconcile status for the table: a Ready condition
// (Flux/ArgoCD GitOps CRs and many others expose one), else a phase (Pods, PVCs, …). Returns null when
// the object carries no recognizable status.
export function resourceStatus(obj: K8sObject): { label: string; ok: boolean } | null {
  const status = obj.status as
    | { conditions?: { type?: string; status?: string; reason?: string }[]; phase?: string }
    | undefined;
  if (!status) {
    return null;
  }

  const ready = status.conditions?.find((condition) => condition.type === "Ready");
  if (ready) {
    if (ready.status === "True") {
      return { label: "Ready", ok: true };
    }

    return { label: ready.reason ? `Not Ready: ${ready.reason}` : "Not Ready", ok: false };
  }

  if (typeof status.phase === "string" && status.phase !== "") {
    const healthy = ["Running", "Active", "Succeeded", "Bound", "Available"].includes(status.phase);

    return { label: status.phase, ok: healthy };
  }

  return null;
}

// compareResources orders two objects for the active sort column. Status sorts by the derived status
// label so equal-health rows group together.
export function compareResources(a: K8sObject, b: K8sObject, key: SortKey): number {
  switch (key) {
    case "name":
      return (a.metadata?.name ?? "").localeCompare(b.metadata?.name ?? "");
    case "namespace":
      return (a.metadata?.namespace ?? "").localeCompare(b.metadata?.namespace ?? "");
    case "status":
      return (resourceStatus(a)?.label ?? "").localeCompare(resourceStatus(b)?.label ?? "");
    case "age":
      return epochMs(a.metadata?.creationTimestamp) - epochMs(b.metadata?.creationTimestamp);
    default:
      return 0;
  }
}

// toYaml serializes an object to YAML for the detail view, falling back to pretty JSON if the object
// somehow cannot be represented as YAML (never expected for Kubernetes objects).
export function toYaml(obj: unknown): string {
  try {
    return yaml.dump(obj, { noRefs: true, sortKeys: false });
  } catch {
    return JSON.stringify(obj, null, 2);
  }
}

// ObjectCondition is the normalized status condition rendered in the detail view's Conditions table.
export type ObjectCondition = { type: string; status: string; reason: string; message: string };

// objectConditions reads status.conditions from an unstructured object (Deployments, Pods, GitOps CRs,
// and many others expose them), returning [] when absent.
export function objectConditions(obj: K8sObject): ObjectCondition[] {
  const status = obj.status as { conditions?: unknown } | undefined;
  const conditions = Array.isArray(status?.conditions) ? status.conditions : [];

  return conditions
    .map((entry) => {
      const cond = (entry ?? {}) as Record<string, unknown>;

      return {
        type: typeof cond.type === "string" ? cond.type : "",
        status: typeof cond.status === "string" ? cond.status : "",
        reason: typeof cond.reason === "string" ? cond.reason : "",
        message: typeof cond.message === "string" ? cond.message : "",
      };
    })
    .filter((cond) => cond.type !== "");
}

// buildResourceTarget assembles the ResourceAction (the address a scale/restart/reconcile/delete
// operates on) for an object in a cluster, splitting the "namespace/name" clusterId into the cluster
// coordinates. Centralizes the splitClusterKey + field projection every write action repeated inline.
export function buildResourceTarget(clusterId: string, kind: string, obj: K8sObject): ResourceAction {
  const [namespace, clusterName] = splitClusterKey(clusterId);

  return {
    namespace,
    name: clusterName,
    kind,
    resourceName: obj.metadata?.name ?? "",
    resourceNamespace: obj.metadata?.namespace,
  };
}
