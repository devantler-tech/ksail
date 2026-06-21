import { createContext, useContext, useMemo } from "react";
import {
  CLUSTER_SCOPED_KINDS,
  RECONCILABLE_KINDS,
  RESOURCE_KINDS,
  RESTARTABLE_KINDS,
  SCALABLE_KINDS,
  type ClusterMeta,
  type ProviderInfo,
  type ResourceKindMeta,
} from "../api.ts";

// MetaContext carries the /api/v1/meta payload (distributions, provider matrix, component options)
// fetched once at app start. The forms render from it instead of hard-coding the cluster schema.
export const MetaContext = createContext<ClusterMeta | null>(null);

export function useMeta(): ClusterMeta {
  const meta = useContext(MetaContext);
  if (!meta) {
    throw new Error("useMeta must be used within a MetaContext provider");
  }
  return meta;
}

// COMPONENT_LABELS maps a component spec key to its human-facing label. This is presentation only:
// the option lists and defaults come from the server's /meta payload, not from here.
export const COMPONENT_LABELS: Record<string, string> = {
  cni: "CNI",
  csi: "CSI",
  cdi: "CDI",
  metricsServer: "Metrics Server",
  loadBalancer: "Load Balancer",
  certManager: "Cert Manager",
  policyEngine: "Policy Engine",
  gitOpsEngine: "GitOps Engine",
};

// preferredProvider picks the provider to pre-select for a distribution in the create form. Which
// provider is the zero-config choice depends on the surface: the local `ksail open web`/desktop backend
// (recognizable by its per-provider availability report) is Docker-first — Docker is KSail's only
// required local dependency — while the operator (no gating report) runs in-cluster, so the
// Kubernetes (nested) provider is its zero-config choice. Falls back to the first provider the
// server lists. This is a UX default only — the full set of valid providers still comes from the
// /meta matrix.
export function preferredProvider(providers: string[], status?: ProviderInfo[] | null): string {
  const preferred = status ? "Docker" : "Kubernetes";

  return providers.includes(preferred) ? preferred : (providers[0] ?? "");
}

// availableProviders narrows the providers valid for a distribution (from /api/v1/meta) to those the
// backend reports as usable (from /api/v1/config). When status is null/undefined the backend does
// not gate by availability (e.g. the operator), so every valid provider is returned unchanged.
export function availableProviders(
  valid: string[],
  status: ProviderInfo[] | null | undefined,
): string[] {
  if (!status) {
    return valid;
  }
  const usable = new Set(status.filter((provider) => provider.available).map((p) => p.name));
  return valid.filter((name) => usable.has(name));
}

// unavailableProviders returns the reported providers that are not usable, for surfacing why an
// option is missing. Empty when the backend does not gate.
export function unavailableProviders(
  status: ProviderInfo[] | null | undefined,
): ProviderInfo[] {
  return (status ?? []).filter((provider) => !provider.available);
}

// ResourceKindLists are the kind lists the workload browser renders from: the kind-selector entries
// and the per-action membership lists (which kinds support scale/restart/reconcile, and which are
// delete-denied).
export interface ResourceKindLists {
  kinds: readonly string[];
  scalable: readonly string[];
  restartable: readonly string[];
  reconcilable: readonly string[];
  deleteDenied: readonly string[];
}

// kindNames projects the kinds matching a predicate to their names.
function kindNames(kinds: ResourceKindMeta[], match: (kind: ResourceKindMeta) => boolean): string[] {
  return kinds.filter(match).map((kind) => kind.kind);
}

// resourceKindLists derives the workload browser's kind lists from /api/v1/meta's resourceKinds —
// the runtime source of truth, mirroring the backend's allowlist and predicates so adding a
// browsable kind in Go needs no TypeScript change. Backends that predate the field (and a
// not-yet-loaded meta) fall back to the hand-maintained constants in api.ts; that fallback is
// permanent so the SPA keeps working against older backends.
export function resourceKindLists(meta: ClusterMeta | null): ResourceKindLists {
  const kinds = meta?.resourceKinds;
  if (!kinds) {
    return {
      kinds: RESOURCE_KINDS,
      scalable: SCALABLE_KINDS,
      restartable: RESTARTABLE_KINDS,
      reconcilable: RECONCILABLE_KINDS,
      deleteDenied: CLUSTER_SCOPED_KINDS,
    };
  }

  return {
    kinds: kindNames(kinds, (kind) => kind.browsable),
    scalable: kindNames(kinds, (kind) => kind.scalable),
    restartable: kindNames(kinds, (kind) => kind.restartable),
    reconcilable: kindNames(kinds, (kind) => kind.reconcilable),
    deleteDenied: kindNames(kinds, (kind) => !kind.deletable),
  };
}

// useResourceKinds resolves the workload browser's kind lists from the MetaContext. Unlike useMeta it
// tolerates a missing meta (it falls back to the constants), so views never crash on an older backend
// or before the meta fetch resolves.
export function useResourceKinds(): ResourceKindLists {
  const meta = useContext(MetaContext);

  return useMemo(() => resourceKindLists(meta), [meta]);
}
