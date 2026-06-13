/**
 * Interactive Prompts
 *
 * Multi-step QuickPick wizards for cluster scaffolding. Enum values and their
 * descriptions come from the single static ENUM_CATALOG (src/ksail/enums.ts),
 * single-sourced against the Go enums — no MCP schema query (that path was
 * removed in Phase 4.3b for hanging ~10s per step).
 *
 * Uses VSCode's multi-step QuickPick pattern with step indicators.
 */

import * as vscode from "vscode";
import {
  describerFor,
  getEnumValues,
  type ClusterInfo,
  type CommonClusterOptions,
  type CreateClusterOptions,
} from "../ksail/index.js";

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
// Enum-Step Helper
// ============================================================================

/**
 * Create QuickPick items from enum values with descriptions.
 * First item is marked as the default.
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

/**
 * Run a single enum selection step backed by ENUM_CATALOG.
 *
 * Collapses the repeated "values → QuickPick items → showMultiStepQuickPick →
 * cancel-check" block that the init and customization wizards previously copied
 * for every field.
 */
function enumStep(
  field: string,
  options: { title: string; step: number; totalSteps: number; placeholder: string }
): Promise<string | undefined> {
  const items = createEnumQuickPickItems(getEnumValues(field), describerFor(field));
  return showMultiStepQuickPick(items, options);
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
    validateInput: (value) => validateClusterName(value),
  });
  if (!name) { return undefined; }

  // Step 3: Distribution (first value is default)
  const distribution = await enumStep("distribution", {
    title,
    step: 3,
    totalSteps,
    placeholder: "Select Kubernetes distribution",
  });
  if (!distribution) { return undefined; }

  // Step 4: Provider (first value is default)
  const provider = await enumStep("provider", {
    title,
    step: 4,
    totalSteps,
    placeholder: "Select infrastructure provider",
  });
  if (!provider) { return undefined; }

  // Step 5: CNI (first value is default)
  const cni = await enumStep("cni", {
    title,
    step: 5,
    totalSteps,
    placeholder: "Select Container Network Interface (CNI)",
  });
  if (!cni) { return undefined; }

  // Step 6: GitOps Engine (first value is default)
  const gitopsEngine = await enumStep("gitops-engine", {
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
      validateInput: (value) => validateClusterName(value),
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
    step: currentStep,
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
  const distribution = await enumStep("distribution", {
    title, step: step++, totalSteps, placeholder: "Select Kubernetes distribution",
  });
  if (!distribution) { return undefined; }

  // Provider
  const provider = await enumStep("provider", {
    title, step: step++, totalSteps, placeholder: "Select infrastructure provider",
  });
  if (!provider) { return undefined; }

  // CNI
  const cni = await enumStep("cni", {
    title, step: step++, totalSteps, placeholder: "Select Container Network Interface (CNI)",
  });
  if (!cni) { return undefined; }

  // CSI
  const csi = await enumStep("csi", {
    title, step: step++, totalSteps, placeholder: "Select Container Storage Interface (CSI)",
  });
  if (!csi) { return undefined; }

  // Metrics Server
  const metricsServer = await enumStep("metrics-server", {
    title, step: step++, totalSteps, placeholder: "Select Metrics Server option",
  });
  if (!metricsServer) { return undefined; }

  // Cert Manager
  const certManager = await enumStep("cert-manager", {
    title, step: step++, totalSteps, placeholder: "Select Cert-Manager option",
  });
  if (!certManager) { return undefined; }

  // Policy Engine
  const policyEngine = await enumStep("policy-engine", {
    title, step: step++, totalSteps, placeholder: "Select Policy Engine",
  });
  if (!policyEngine) { return undefined; }

  // GitOps Engine
  const gitopsEngine = await enumStep("gitops-engine", {
    title, step: step, totalSteps, placeholder: "Select GitOps engine",
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

// ============================================================================
// Simple Prompts (for non-wizard use cases)
// ============================================================================

/**
 * Validate a cluster name against DNS-1123 label rules.
 * Returns an error message, or undefined when the name is valid.
 */
function validateClusterName(value: string): string | undefined {
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
}

/**
 * Prompt for cluster name with DNS-1123 validation
 */
export async function promptClusterName(defaultValue?: string): Promise<string | undefined> {
  const name = await vscode.window.showInputBox({
    prompt: "Enter cluster name",
    placeHolder: "my-cluster",
    value: defaultValue,
    validateInput: (value) => validateClusterName(value),
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
