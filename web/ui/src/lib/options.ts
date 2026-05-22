// Enum option lists for cluster component fields, mirroring the ksail API enums
// (pkg/apis/cluster/v1alpha1). The first entry is the API default (its zero value), so leaving a
// field at the first option is equivalent to not setting it.

const TOGGLE_DEFAULT = ["Default", "Enabled", "Disabled"];

export interface ComponentField {
  key: "cni" | "csi" | "cdi" | "metricsServer" | "loadBalancer" | "certManager" | "policyEngine" | "gitOpsEngine";
  label: string;
  options: string[];
  help?: string;
}

// COMPONENT_FIELDS drives the "Advanced" section of the cluster form. Order roughly follows the
// install order (networking/storage first, then add-ons, then GitOps).
export const COMPONENT_FIELDS: ComponentField[] = [
  { key: "cni", label: "CNI", options: ["Default", "Cilium", "Calico"] },
  { key: "csi", label: "CSI", options: TOGGLE_DEFAULT },
  { key: "cdi", label: "CDI", options: TOGGLE_DEFAULT },
  { key: "metricsServer", label: "Metrics Server", options: TOGGLE_DEFAULT },
  { key: "loadBalancer", label: "Load Balancer", options: TOGGLE_DEFAULT },
  { key: "certManager", label: "Cert Manager", options: ["Enabled", "Disabled"] },
  { key: "policyEngine", label: "Policy Engine", options: ["None", "Kyverno", "Gatekeeper"] },
  { key: "gitOpsEngine", label: "GitOps Engine", options: ["None", "Flux", "ArgoCD"] },
];

// componentDefaults returns the default selection for every component field (the first option).
export function componentDefaults(): Record<ComponentField["key"], string> {
  const defaults = {} as Record<ComponentField["key"], string>;
  for (const field of COMPONENT_FIELDS) {
    defaults[field.key] = field.options[0];
  }
  return defaults;
}
