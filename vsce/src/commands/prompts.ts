/**
 * Interactive Prompts
 *
 * Dynamic input helpers that query MCP tool schemas for valid values.
 * This ensures the extension stays in sync with CLI capabilities automatically.
 */

import * as vscode from "vscode";
import type { ClusterInfo } from "../ksail/index.js";
import { getEnumValues, getPropertyDescription } from "../mcp/index.js";

/**
 * MCP tool name for cluster init command
 */
const CLUSTER_INIT_TOOL = "cluster_init";

/**
 * Fallback enum values when MCP schema is unavailable
 */
const FALLBACK_VALUES: Record<string, string[]> = {
  distribution: ["Vanilla", "K3s", "Talos"],
  provider: ["Docker", "Hetzner"],
  cni: ["Default", "Cilium", "Calico"],
  csi: ["Default", "Enabled", "Disabled"],
  metrics_server: ["Default", "Enabled", "Disabled"],
  cert_manager: ["Enabled", "Disabled"],
  policy_engine: ["None", "Kyverno", "Gatekeeper"],
  gitops_engine: ["Flux", "ArgoCD", "None"],
};

/**
 * Property descriptions for user-friendly prompts
 */
const PROPERTY_DESCRIPTIONS: Record<string, string> = {
  distribution: "Kubernetes distribution",
  provider: "Infrastructure provider",
  cni: "Container Network Interface (CNI)",
  csi: "Container Storage Interface (CSI)",
  metrics_server: "Metrics Server",
  cert_manager: "Cert-Manager",
  policy_engine: "Policy Engine",
  gitops_engine: "GitOps Engine",
};

/**
 * Prompt for cluster name with DNS-1123 validation
 */
export async function promptClusterName(defaultValue?: string): Promise<string | undefined> {
  const name = await vscode.window.showInputBox({
    prompt: "Enter cluster name",
    placeHolder: "my-cluster",
    value: defaultValue,
    validateInput: (value) => {
      if (!value) {
        return "Cluster name is required";
      }
      // DNS-1123 validation
      if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/.test(value)) {
        return "Must be lowercase alphanumeric, may contain hyphens, cannot start/end with hyphen";
      }
      if (value.length > 63) {
        return "Must be 63 characters or less";
      }
      return undefined;
    },
  });

  return name;
}

/**
 * Prompt for cluster selection from a list
 */
export async function promptClusterSelection(
  clusters: ClusterInfo[],
  title?: string
): Promise<ClusterInfo | undefined> {
  if (clusters.length === 0) {
    vscode.window.showWarningMessage("No clusters found");
    return undefined;
  }

  const items = clusters.map((c) => ({
    label: c.name,
    description: `${c.distribution} - ${c.status}`,
    cluster: c,
  }));

  const selected = await vscode.window.showQuickPick(items, {
    placeHolder: title ?? "Select a cluster",
  });

  return selected?.cluster;
}

/**
 * Get enum values from MCP schema with fallback
 */
async function getSchemaEnumValues(propertyName: string): Promise<string[]> {
  try {
    const values = await getEnumValues(CLUSTER_INIT_TOOL, propertyName);
    if (values && values.length > 0) {
      return values;
    }
  } catch {
    // MCP unavailable, use fallback
  }
  return FALLBACK_VALUES[propertyName] || [];
}

/**
 * Get property description from MCP schema with fallback
 */
async function getSchemaDescription(propertyName: string): Promise<string> {
  try {
    const description = await getPropertyDescription(CLUSTER_INIT_TOOL, propertyName);
    if (description) {
      return description;
    }
  } catch {
    // MCP unavailable, use fallback
  }
  return PROPERTY_DESCRIPTIONS[propertyName] || propertyName;
}

/**
 * Generic schema-driven enum prompt
 *
 * Queries the MCP schema for valid enum values and presents them as a QuickPick.
 */
async function promptSchemaEnum(
  propertyName: string,
  placeholder?: string
): Promise<string | undefined> {
  const values = await getSchemaEnumValues(propertyName);
  const description = placeholder || await getSchemaDescription(propertyName);

  if (values.length === 0) {
    vscode.window.showErrorMessage(`No options available for ${propertyName}`);
    return undefined;
  }

  const items = values.map((v) => ({ label: v, value: v }));

  const selected = await vscode.window.showQuickPick(items, {
    placeHolder: `Select ${description}`,
  });

  return selected?.value;
}

/**
 * Prompt for distribution selection (schema-driven)
 */
export async function promptDistribution(): Promise<string | undefined> {
  return promptSchemaEnum("distribution", "Kubernetes distribution");
}

/**
 * Prompt for provider selection (schema-driven)
 */
export async function promptProvider(): Promise<string | undefined> {
  return promptSchemaEnum("provider", "Infrastructure provider");
}

/**
 * Prompt for CNI selection (schema-driven)
 */
export async function promptCNI(): Promise<string | undefined> {
  return promptSchemaEnum("cni", "Container Network Interface (CNI)");
}

/**
 * Prompt for GitOps engine selection (schema-driven)
 */
export async function promptGitOpsEngine(): Promise<string | undefined> {
  return promptSchemaEnum("gitops_engine", "GitOps Engine");
}

/**
 * Prompt for yes/no confirmation
 */
export async function promptYesNo(
  message: string,
  yesLabel = "Yes",
  noLabel = "No"
): Promise<boolean | undefined> {
  const items = [
    { label: yesLabel, value: true },
    { label: noLabel, value: false },
  ];

  const selected = await vscode.window.showQuickPick(items, {
    placeHolder: message,
  });

  return selected?.value;
}

/**
 * Prompt for number input
 */
export async function promptNumber(
  prompt: string,
  defaultValue: number,
  min = 1,
  max = 100
): Promise<number | undefined> {
  const value = await vscode.window.showInputBox({
    prompt,
    value: defaultValue.toString(),
    validateInput: (input) => {
      const num = parseInt(input, 10);
      if (isNaN(num)) {
        return "Must be a number";
      }
      if (num < min || num > max) {
        return `Must be between ${min} and ${max}`;
      }
      return undefined;
    },
  });

  return value ? parseInt(value, 10) : undefined;
}

/**
 * Advanced options for cluster init (schema-driven)
 */
export interface AdvancedInitOptions {
  csi?: string;
  metricsServer?: string;
  certManager?: string;
  policyEngine?: string;
  gitopsEngine?: string;
}

/**
 * Prompt for advanced cluster init options (schema-driven)
 */
export async function promptAdvancedOptions(): Promise<AdvancedInitOptions | undefined> {
  const configure = await promptYesNo(
    "Configure advanced options?",
    "Yes, configure",
    "No, use defaults"
  );

  if (!configure) {
    return {};
  }

  const options: AdvancedInitOptions = {};

  // CSI (schema-driven)
  const csi = await promptSchemaEnum("csi", "Container Storage Interface (CSI)");
  if (csi === undefined) { return undefined; }
  options.csi = csi;

  // Metrics Server (schema-driven)
  const metrics = await promptSchemaEnum("metrics_server", "Metrics Server");
  if (metrics === undefined) { return undefined; }
  options.metricsServer = metrics;

  // Cert Manager (schema-driven)
  const cert = await promptSchemaEnum("cert_manager", "Cert-Manager");
  if (cert === undefined) { return undefined; }
  options.certManager = cert;

  // Policy Engine (schema-driven)
  const policy = await promptSchemaEnum("policy_engine", "Policy Engine");
  if (policy === undefined) { return undefined; }
  options.policyEngine = policy;

  // GitOps Engine (schema-driven)
  const gitops = await promptSchemaEnum("gitops_engine", "GitOps Engine");
  if (gitops === undefined) { return undefined; }
  options.gitopsEngine = gitops;

  return options;
}
