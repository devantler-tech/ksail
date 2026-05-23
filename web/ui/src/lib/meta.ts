import { createContext, useContext } from "react";
import type { ClusterMeta } from "../api.ts";

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

// preferredProvider picks the provider to pre-select for a distribution in the create form. The
// operator runs in-cluster, so the Kubernetes (nested) provider is the zero-config choice when a
// distribution supports it; otherwise fall back to the first provider the server lists. This is a
// UX default only — the full set of valid providers still comes from the /meta matrix.
export function preferredProvider(providers: string[]): string {
  return providers.includes("Kubernetes") ? "Kubernetes" : (providers[0] ?? "");
}
