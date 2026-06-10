import { createContext, useContext } from "react";
import type { ClusterMeta, ProviderInfo } from "../api.ts";

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
// provider is the zero-config choice depends on the surface: the local `ksail ui`/desktop backend
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
