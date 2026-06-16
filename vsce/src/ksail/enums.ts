/**
 * KSail Enum Catalog (single source for the VSCE wizards)
 *
 * One place describing the selectable values for cluster scaffolding. Both the
 * QuickPick wizard (commands/prompts.ts) and the HTML "Create Cluster" wizard
 * (kubernetes/clusterProvider.ts) import these — no second, drifting copy.
 *
 * DRIFT GUARD — the option *values* mirror the Go enums; keep them in lockstep:
 *   - distribution → pkg/apis/cluster/v1alpha1/distribution.go (ValidDistributions)
 *       Vanilla, K3s, Talos, VCluster, KWOK, EKS
 *   - provider     → pkg/apis/cluster/v1alpha1/provider.go (ValidProviders)
 *       Docker, Hetzner, Omni, AWS, Kubernetes
 *   - cni/csi/metrics-server/cert-manager/policy-engine/gitops-engine →
 *       the matching enum files under pkg/apis/cluster/v1alpha1/.
 * Go enums carry no per-value description, so the human descriptions below are
 * hand-curated and must be updated by hand when a value is added/removed.
 * The first value in each array is the CLI default.
 *
 * This module is intentionally vscode-free so it can be unit-tested directly.
 */

/**
 * An enum field's selectable values and a description for each value.
 */
export interface EnumCatalogEntry {
  /** Allowed values; index 0 is the CLI default. */
  values: string[];
  /** Per-value human descriptions (may be partial). */
  descriptions: Record<string, string>;
}

/**
 * The full catalog keyed by CLI flag name (matching `cluster init`/`create` flags).
 */
export const ENUM_CATALOG: Record<string, EnumCatalogEntry> = {
  distribution: {
    values: ["Vanilla", "K3s", "Talos", "VCluster", "KWOK", "EKS"],
    descriptions: {
      Vanilla: "Standard upstream Kubernetes via Kind",
      K3s: "Lightweight Kubernetes via K3d",
      Talos: "Immutable Talos Linux Kubernetes",
      VCluster: "Virtual clusters via vCluster (Vind) in Docker",
      KWOK: "Simulated Kubernetes cluster (no real workloads)",
      EKS: "Managed Kubernetes on Amazon Web Services",
    },
  },
  provider: {
    values: ["Docker", "Hetzner", "Omni", "AWS", "Kubernetes"],
    descriptions: {
      Docker: "Run cluster nodes as Docker containers",
      Hetzner: "Run cluster nodes as Hetzner Cloud servers",
      Omni: "Manage Talos nodes through Sidero Omni",
      AWS: "Provision managed EKS clusters on AWS",
      Kubernetes: "Run nested clusters inside an existing Kubernetes cluster",
    },
  },
  cni: {
    values: ["Default", "Cilium", "Calico"],
    descriptions: {
      Default: "Use the distribution's default CNI",
      Cilium: "eBPF-based networking with advanced features",
      Calico: "Flexible networking and network policy",
    },
  },
  csi: {
    values: ["Default", "Enabled", "Disabled"],
    descriptions: {
      Default: "Use the distribution's default CSI",
      Enabled: "Enable local path provisioner",
      Disabled: "No CSI provisioner",
    },
  },
  "metrics-server": {
    values: ["Default", "Enabled", "Disabled"],
    descriptions: {
      Default: "Use the distribution's default metrics-server setting",
      Enabled: "Install metrics-server",
      Disabled: "Do not install metrics-server",
    },
  },
  "cert-manager": {
    values: ["Disabled", "Enabled"],
    descriptions: {
      Disabled: "No cert-manager",
      Enabled: "Install cert-manager",
    },
  },
  "policy-engine": {
    values: ["None", "Kyverno", "Gatekeeper"],
    descriptions: {
      None: "No policy engine",
      Kyverno: "Kubernetes native policy management",
      Gatekeeper: "OPA-based policy controller",
    },
  },
  "gitops-engine": {
    values: ["None", "Flux", "ArgoCD"],
    descriptions: {
      None: "No GitOps engine installed",
      Flux: "Flux CD - GitOps toolkit for Kubernetes",
      ArgoCD: "Argo CD - Declarative GitOps for Kubernetes",
    },
  },
};

/**
 * Get the selectable values for an enum field (defaults to [] for unknown fields).
 */
export function getEnumValues(field: string): string[] {
  return ENUM_CATALOG[field]?.values ?? [];
}

/**
 * Get the human description for a single enum value (empty string when none).
 */
export function getEnumDescription(field: string, value: string): string {
  return ENUM_CATALOG[field]?.descriptions[value] ?? "";
}

/**
 * Build a description lookup function bound to a single enum field, for callers
 * that take a `(value) => string` describer (e.g. QuickPick item builders).
 */
export function describerFor(field: string): (value: string) => string {
  return (value: string): string => getEnumDescription(field, value);
}
