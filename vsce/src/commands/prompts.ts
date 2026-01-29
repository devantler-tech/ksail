/**
 * Interactive Prompts
 *
 * Dynamic input helpers that query MCP tool schemas for valid values.
 * This ensures the extension stays in sync with CLI capabilities automatically.
 *
 * Uses VSCode's multi-step QuickPick pattern with step indicators.
 */

import * as vscode from "vscode";
import type { ClusterInfo, CommonClusterOptions, CreateClusterOptions } from "../ksail/index.js";
import { getEnumValues } from "../mcp/index.js";

/**
 * MCP tool name for cluster init command
 */
const CLUSTER_INIT_TOOL = "cluster_init";

/**
 * Fallback enum values when MCP schema is unavailable
 * First value in each array is the CLI default
 */
const FALLBACK_VALUES: Record<string, string[]> = {
  distribution: ["Vanilla", "K3s", "Talos"],
  provider: ["Docker", "Hetzner"],
  cni: ["Default", "Cilium", "Calico"],
  csi: ["Default", "Enabled", "Disabled"],
  metrics_server: ["Default", "Enabled", "Disabled"],
  cert_manager: ["Disabled", "Enabled"],
  policy_engine: ["None", "Kyverno", "Gatekeeper"],
  gitops_engine: ["None", "Flux", "ArgoCD"],
};

// ============================================================================
// Multi-Step Input Helper
// ============================================================================

/**
 * QuickPick item with value
 */
interface QuickPickItemWithValue<T> extends vscode.QuickPickItem {
  value: T;
}

/**
 * Create a multi-step QuickPick
 */
async function showMultiStepQuickPick<T>(
  items: QuickPickItemWithValue<T>[],
  options: {
    title: string;
    step: number;
    totalSteps: number;
    placeholder?: string;
  }
): Promise<T | undefined> {
  const quickPick = vscode.window.createQuickPick<QuickPickItemWithValue<T>>();
  quickPick.title = `${options.title} (${options.step}/${options.totalSteps})`;
  quickPick.placeholder = options.placeholder;
  quickPick.items = items;
  quickPick.ignoreFocusOut = true;

  // Pre-select the first item (which is the default)
  if (items.length > 0) {
    quickPick.activeItems = [items[0]];
  }

  return new Promise<T | undefined>((resolve) => {
    quickPick.onDidAccept(() => {
      const selection = quickPick.selectedItems[0];
      quickPick.hide();
      resolve(selection?.value);
    });
    quickPick.onDidHide(() => {
      quickPick.dispose();
      resolve(undefined);
    });
    quickPick.show();
  });
}

/**
 * Create a multi-step InputBox
 */
async function showMultiStepInputBox(options: {
  title: string;
  step: number;
  totalSteps: number;
  prompt?: string;
  placeholder?: string;
  value?: string;
  validateInput?: (value: string) => string | undefined;
}): Promise<string | undefined> {
  const inputBox = vscode.window.createInputBox();
  inputBox.title = `${options.title} (${options.step}/${options.totalSteps})`;
  inputBox.prompt = options.prompt;
  inputBox.placeholder = options.placeholder;
  inputBox.value = options.value ?? "";
  inputBox.ignoreFocusOut = true;

  return new Promise<string | undefined>((resolve) => {
    inputBox.onDidAccept(() => {
      const value = inputBox.value;
      if (options.validateInput) {
        const error = options.validateInput(value);
        if (error) {
          inputBox.validationMessage = error;
          return;
        }
      }
      inputBox.hide();
      resolve(value);
    });
    inputBox.onDidChangeValue((value) => {
      if (options.validateInput) {
        inputBox.validationMessage = options.validateInput(value);
      }
    });
    inputBox.onDidHide(() => {
      inputBox.dispose();
      resolve(undefined);
    });
    inputBox.show();
  });
}

// ============================================================================
// Schema-Driven Helpers
// ============================================================================

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
 * Create QuickPick items from enum values with descriptions
 * First item is marked as default
 */
function createEnumQuickPickItems(
  values: string[],
  getDescription: (value: string) => string
): QuickPickItemWithValue<string>[] {
  return values.map((v, i) => ({
    label: v,
    description: getDescription(v) + (i === 0 ? " (default)" : ""),
    value: v,
    picked: i === 0,
  }));
}

// ============================================================================
// Cluster Init Wizard
// ============================================================================

/**
 * Full cluster init options
 */
export interface ClusterInitOptions extends CommonClusterOptions {
  outputPath: string;
}

/**
 * Run the cluster init wizard
 *
 * Multi-step QuickPick flow with step indicators.
 * Returns undefined if user cancels at any step.
 */
export async function runClusterInitWizard(): Promise<ClusterInitOptions | undefined> {
  const title = "KSail: Initialize Cluster";
  const totalSteps = 6;

  // Get workspace folder for default output path
  const workspaceFolder = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath ?? ".";

  // Step 1: Output path
  const outputPath = await showMultiStepInputBox({
    title,
    step: 1,
    totalSteps,
    prompt: "Enter output directory for cluster configuration",
    placeholder: workspaceFolder,
    value: workspaceFolder,
  });
  if (outputPath === undefined) { return undefined; }

  // Step 2: Cluster name
  const name = await showMultiStepInputBox({
    title,
    step: 2,
    totalSteps,
    prompt: "Enter a name for your cluster",
    placeholder: "my-cluster",
    value: "local",
    validateInput: (value) => {
      if (!value) {
        return "Cluster name is required";
      }
      if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/.test(value)) {
        return "Must be lowercase alphanumeric, may contain hyphens, cannot start/end with hyphen";
      }
      if (value.length > 63) {
        return "Must be 63 characters or less";
      }
      return undefined;
    },
  });
  if (!name) { return undefined; }

  // Step 3: Distribution (first value is default)
  const distributionValues = await getSchemaEnumValues("distribution");
  const distributionItems = createEnumQuickPickItems(distributionValues, getDistributionDescription);
  const distribution = await showMultiStepQuickPick(distributionItems, {
    title,
    step: 3,
    totalSteps,
    placeholder: "Select Kubernetes distribution",
  });
  if (!distribution) { return undefined; }

  // Step 4: Provider (first value is default)
  const providerValues = await getSchemaEnumValues("provider");
  const providerItems = createEnumQuickPickItems(providerValues, getProviderDescription);
  const provider = await showMultiStepQuickPick(providerItems, {
    title,
    step: 4,
    totalSteps,
    placeholder: "Select infrastructure provider",
  });
  if (!provider) { return undefined; }

  // Step 5: CNI (first value is default)
  const cniValues = await getSchemaEnumValues("cni");
  const cniItems = createEnumQuickPickItems(cniValues, getCniDescription);
  const cni = await showMultiStepQuickPick(cniItems, {
    title,
    step: 5,
    totalSteps,
    placeholder: "Select Container Network Interface (CNI)",
  });
  if (!cni) { return undefined; }

  // Step 6: GitOps Engine (first value is default)
  const gitopsValues = await getSchemaEnumValues("gitops_engine");
  const gitopsItems = createEnumQuickPickItems(gitopsValues, getGitopsDescription);
  const gitopsEngine = await showMultiStepQuickPick(gitopsItems, {
    title,
    step: 6,
    totalSteps,
    placeholder: "Select GitOps engine",
  });
  if (!gitopsEngine) { return undefined; }

  return { outputPath, name: name!, distribution: distribution!, provider: provider!, cni: cni!, gitopsEngine: gitopsEngine! };
}

// ============================================================================
// Cluster Create Wizard
// ============================================================================

/**
 * Run the cluster create wizard
 *
 * Multi-step QuickPick flow with step indicators.
 * The CLI uses ksail.yaml from the current working directory automatically.
 * Returns undefined if user cancels at any step.
 */
export async function runClusterCreateWizard(): Promise<CreateClusterOptions | undefined> {
  const title = "KSail: Create Cluster";

  // Check if ksail.yaml exists in workspace
  const ksailYamlExists = await vscode.workspace.findFiles("ksail.yaml", null, 1);
  const hasKsailYaml = ksailYamlExists.length > 0;

  // If no ksail.yaml, prompt user to init first or continue with defaults
  if (!hasKsailYaml) {
    const noConfigItems: QuickPickItemWithValue<"defaults" | "customize" | "init">[] = [
      {
        label: "$(play) Create with CLI defaults",
        description: "Use KSail's built-in default settings",
        value: "defaults",
        picked: true,
      },
      {
        label: "$(settings-gear) Customize options...",
        description: "Configure distribution, provider, CNI, and more",
        value: "customize",
      },
      {
        label: "$(add) Initialize first...",
        description: "Create a ksail.yaml configuration file",
        value: "init",
      },
    ];

    const noConfigChoice = await showMultiStepQuickPick(noConfigItems, {
      title,
      step: 1,
      totalSteps: 1,
      placeholder: "No ksail.yaml found. How would you like to proceed?",
    });

    if (!noConfigChoice) { return undefined; }

    if (noConfigChoice === "init") {
      // Trigger init command instead
      await vscode.commands.executeCommand("ksail.cluster.init");
      return undefined;
    }

    if (noConfigChoice === "defaults") {
      // Use CLI defaults with no options
      return {};
    }

    // Continue to full customization wizard
    return runFullCustomizationWizard(title, 2);
  }

  // ksail.yaml exists - show simplified wizard
  const totalSteps = 3;
  let currentStep = 1;

  // Step 1: Distribution config path (optional)
  const distConfigItems: QuickPickItemWithValue<string | "browse" | "none">[] = [
    {
      label: "$(dash) Use default from ksail.yaml",
      description: "Let KSail determine the distribution config (default)",
      value: "none",
      picked: true,
    },
    {
      label: "$(folder-opened) Browse for distribution config...",
      description: "Select kind.yaml, k3d.yaml, or Talos config",
      value: "browse",
    },
  ];

  const distConfigSelection = await showMultiStepQuickPick(distConfigItems, {
    title,
    step: currentStep++,
    totalSteps,
    placeholder: "Select distribution configuration (optional)",
  });
  if (distConfigSelection === undefined) { return undefined; }

  let distributionConfigPath: string | undefined;
  if (distConfigSelection === "browse") {
    const result = await vscode.window.showOpenDialog({
      canSelectFiles: true,
      canSelectFolders: false,
      canSelectMany: false,
      filters: { "YAML files": ["yaml", "yml"] },
      title: "Select distribution config (kind.yaml, k3d.yaml, etc.)",
    });
    if (!result || result.length === 0) { return undefined; }
    distributionConfigPath = result[0].fsPath;
  }

  // Step 2: Cluster name (optional override)
  const nameItems: QuickPickItemWithValue<string | "custom">[] = [
    {
      label: "$(dash) Use name from ksail.yaml",
      description: "Use the cluster name defined in the config (default)",
      value: "",
      picked: true,
    },
    {
      label: "$(pencil) Enter custom name...",
      description: "Override the cluster name",
      value: "custom",
    },
  ];

  const nameSelection = await showMultiStepQuickPick(nameItems, {
    title,
    step: currentStep++,
    totalSteps,
    placeholder: "Cluster name",
  });
  if (nameSelection === undefined) { return undefined; }

  let name: string | undefined;
  if (nameSelection === "custom") {
    name = await vscode.window.showInputBox({
      prompt: "Enter cluster name",
      placeHolder: "my-cluster",
      validateInput: (value) => {
        if (!value) { return "Cluster name is required"; }
        if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/.test(value)) {
          return "Must be lowercase alphanumeric, may contain hyphens";
        }
        return undefined;
      },
    });
    if (!name) { return undefined; }
  }

  // Step 3: Use defaults or customize
  const defaultsItems: QuickPickItemWithValue<boolean>[] = [
    {
      label: "$(check) Use defaults from ksail.yaml",
      description: "Create cluster with settings from your configuration",
      value: true,
      picked: true,
    },
    {
      label: "$(settings-gear) Customize options...",
      description: "Override distribution, provider, CNI, and more",
      value: false,
    },
  ];

  const useDefaults = await showMultiStepQuickPick(defaultsItems, {
    title,
    step: currentStep++,
    totalSteps,
    placeholder: "Use default settings?",
  });
  if (useDefaults === undefined) { return undefined; }

  // If using defaults, return minimal options
  if (useDefaults) {
    return {
      distributionConfigPath,
      name: name || undefined,
    };
  }

  // Continue to full customization
  return runFullCustomizationWizard(title, 4, distributionConfigPath, name);
}

/**
 * Run the full customization wizard for cluster creation
 */
async function runFullCustomizationWizard(
  title: string,
  startStep: number,
  distributionConfigPath?: string,
  name?: string
): Promise<CreateClusterOptions | undefined> {
  const totalSteps = startStep + 9; // 10 more steps for customization
  let step = startStep;

  // Distribution
  const distributionValues = await getSchemaEnumValues("distribution");
  const distributionItems = createEnumQuickPickItems(distributionValues, getDistributionDescription);
  const distribution = await showMultiStepQuickPick(distributionItems, {
    title,
    step: step++,
    totalSteps,
    placeholder: "Select Kubernetes distribution",
  });
  if (!distribution) { return undefined; }

  // Provider
  const providerValues = await getSchemaEnumValues("provider");
  const providerItems = createEnumQuickPickItems(providerValues, getProviderDescription);
  const provider = await showMultiStepQuickPick(providerItems, {
    title,
    step: step++,
    totalSteps,
    placeholder: "Select infrastructure provider",
  });
  if (!provider) { return undefined; }

  // CNI
  const cniValues = await getSchemaEnumValues("cni");
  const cniItems = createEnumQuickPickItems(cniValues, getCniDescription);
  const cni = await showMultiStepQuickPick(cniItems, {
    title,
    step: step++,
    totalSteps,
    placeholder: "Select Container Network Interface (CNI)",
  });
  if (!cni) { return undefined; }

  // CSI
  const csiValues = await getSchemaEnumValues("csi");
  const csiItems = createEnumQuickPickItems(csiValues, getCsiDescription);
  const csi = await showMultiStepQuickPick(csiItems, {
    title,
    step: step++,
    totalSteps,
    placeholder: "Select Container Storage Interface (CSI)",
  });
  if (!csi) { return undefined; }

  // Metrics Server
  const metricsValues = await getSchemaEnumValues("metrics_server");
  const metricsItems = createEnumQuickPickItems(metricsValues, () => "");
  const metricsServer = await showMultiStepQuickPick(metricsItems, {
    title,
    step: step++,
    totalSteps,
    placeholder: "Select Metrics Server option",
  });
  if (!metricsServer) { return undefined; }

  // Cert Manager
  const certValues = await getSchemaEnumValues("cert_manager");
  const certItems = createEnumQuickPickItems(certValues, () => "");
  const certManager = await showMultiStepQuickPick(certItems, {
    title,
    step: step++,
    totalSteps,
    placeholder: "Select Cert-Manager option",
  });
  if (!certManager) { return undefined; }

  // Policy Engine
  const policyValues = await getSchemaEnumValues("policy_engine");
  const policyItems = createEnumQuickPickItems(policyValues, getPolicyDescription);
  const policyEngine = await showMultiStepQuickPick(policyItems, {
    title,
    step: step++,
    totalSteps,
    placeholder: "Select Policy Engine",
  });
  if (!policyEngine) { return undefined; }

  // GitOps Engine
  const gitopsValues = await getSchemaEnumValues("gitops_engine");
  const gitopsItems = createEnumQuickPickItems(gitopsValues, getGitopsDescription);
  const gitopsEngine = await showMultiStepQuickPick(gitopsItems, {
    title,
    step: step++,
    totalSteps,
    placeholder: "Select GitOps engine",
  });
  if (!gitopsEngine) { return undefined; }

  // Control planes count
  const controlPlanes = await promptNumber(
    "Number of control plane nodes",
    1,
    1,
    10
  );
  if (controlPlanes === undefined) { return undefined; }

  // Workers count
  const workers = await promptNumber(
    "Number of worker nodes",
    0,
    0,
    100
  );
  if (workers === undefined) { return undefined; }

  return {
    distributionConfigPath,
    name: name || undefined,
    distribution,
    provider,
    cni,
    csi,
    metricsServer,
    certManager,
    policyEngine,
    gitopsEngine,
    controlPlanes,
    workers,
  };
}

/**
 * Get description for distribution options
 */
function getDistributionDescription(distribution: string): string {
  const descriptions: Record<string, string> = {
    Vanilla: "Standard upstream Kubernetes via Kind",
    K3s: "Lightweight Kubernetes via K3d",
    Talos: "Immutable Talos Linux Kubernetes",
  };
  return descriptions[distribution] || "";
}

/**
 * Get description for provider options
 */
function getProviderDescription(provider: string): string {
  const descriptions: Record<string, string> = {
    Docker: "Run cluster nodes as Docker containers",
    Hetzner: "Run cluster on Hetzner Cloud servers",
  };
  return descriptions[provider] || "";
}

/**
 * Get description for CNI options
 */
function getCniDescription(cni: string): string {
  const descriptions: Record<string, string> = {
    Default: "Use the distribution's default CNI",
    Cilium: "eBPF-based networking with advanced features",
    Calico: "Flexible networking and network policy",
  };
  return descriptions[cni] || "";
}

/**
 * Get description for CSI options
 */
function getCsiDescription(csi: string): string {
  const descriptions: Record<string, string> = {
    Default: "Use the distribution's default CSI",
    Enabled: "Enable local path provisioner",
    Disabled: "No CSI provisioner",
  };
  return descriptions[csi] || "";
}

/**
 * Get description for policy options
 */
function getPolicyDescription(policy: string): string {
  const descriptions: Record<string, string> = {
    None: "No policy engine",
    Kyverno: "Kubernetes native policy management",
    Gatekeeper: "OPA-based policy controller",
  };
  return descriptions[policy] || "";
}

/**
 * Get description for GitOps options
 */
function getGitopsDescription(gitops: string): string {
  const descriptions: Record<string, string> = {
    Flux: "Flux CD - GitOps toolkit for Kubernetes",
    ArgoCD: "Argo CD - Declarative GitOps for Kubernetes",
    None: "No GitOps engine installed",
  };
  return descriptions[gitops] || "";
}

// ============================================================================
// Simple Prompts (for non-wizard use cases)
// ============================================================================

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
    description: c.provider,
    cluster: c,
  }));

  const selected = await vscode.window.showQuickPick(items, {
    placeHolder: title ?? "Select a cluster",
  });

  return selected?.cluster;
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
