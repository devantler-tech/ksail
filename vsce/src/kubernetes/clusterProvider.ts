/**
 * Kubernetes Cluster Provider
 *
 * Registers KSail in the Kubernetes extension's "Create Cluster" wizard.
 * When the user clicks "Create Cluster" in the Clusters view three-dot menu
 * and selects "KSail", an HTML-based wizard guides them through configuration.
 */

import * as vscode from "vscode";
import { ClusterProviderV1 } from "vscode-kubernetes-tools-api";
import { createCluster } from "../ksail/index.js";
import { getEnumValues } from "../mcp/index.js";

/**
 * Fallback enum values when MCP schema is unavailable.
 * First value in each array is the CLI default.
 */
const FALLBACK_VALUES: Record<string, string[]> = {
  distribution: ["Vanilla", "K3s", "Talos", "VCluster"],
  provider: ["Docker", "Hetzner", "Omni"],
  cni: ["Default", "Cilium", "Calico"],
  "gitops-engine": ["None", "Flux", "ArgoCD"],
};

const CLUSTER_INIT_TOOL = "cluster_init";

/**
 * Step identifiers for the wizard flow
 */
const STEPS = {
  CONFIGURE: "ksail-configure",
  CREATING: "ksail-creating",
} as const;

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
 * Generate VS Code themed CSS for the wizard HTML pages
 */
function getWizardStyles(): string {
  return `
    <style>
      body {
        font-family: var(--vscode-font-family);
        font-size: var(--vscode-font-size);
        color: var(--vscode-foreground);
        background-color: var(--vscode-editor-background);
        padding: 20px;
        max-width: 600px;
        margin: 0 auto;
      }
      h2 {
        color: var(--vscode-foreground);
        margin-bottom: 20px;
        font-weight: 600;
      }
      label {
        display: block;
        margin-bottom: 4px;
        font-weight: 500;
        color: var(--vscode-foreground);
      }
      .field {
        margin-bottom: 16px;
      }
      .field-description {
        font-size: 0.85em;
        color: var(--vscode-descriptionForeground);
        margin-bottom: 4px;
      }
      select, input[type="text"], input[type="number"] {
        width: 100%;
        padding: 6px 8px;
        border: 1px solid var(--vscode-input-border);
        background-color: var(--vscode-input-background);
        color: var(--vscode-input-foreground);
        border-radius: 2px;
        font-size: var(--vscode-font-size);
        font-family: var(--vscode-font-family);
        box-sizing: border-box;
      }
      select:focus, input:focus {
        outline: 1px solid var(--vscode-focusBorder);
        border-color: var(--vscode-focusBorder);
      }
      button {
        padding: 8px 16px;
        border: none;
        border-radius: 2px;
        cursor: pointer;
        font-size: var(--vscode-font-size);
        font-family: var(--vscode-font-family);
      }
      .btn-primary {
        background-color: var(--vscode-button-background);
        color: var(--vscode-button-foreground);
      }
      .btn-primary:hover {
        background-color: var(--vscode-button-hoverBackground);
      }
      .actions {
        margin-top: 24px;
        display: flex;
        gap: 8px;
      }
      .progress {
        text-align: center;
        padding: 40px 20px;
      }
      .progress p {
        margin-top: 16px;
        color: var(--vscode-descriptionForeground);
      }
      .spinner {
        display: inline-block;
        width: 32px;
        height: 32px;
        border: 3px solid var(--vscode-input-border);
        border-top: 3px solid var(--vscode-button-background);
        border-radius: 50%;
        animation: spin 1s linear infinite;
      }
      @keyframes spin {
        to { transform: rotate(360deg); }
      }
    </style>
  `;
}

/**
 * Build a select dropdown HTML string
 */
function buildSelect(name: string, values: string[], defaultIndex = 0): string {
  const options = values.map((v, i) =>
    `<option value="${v}"${i === defaultIndex ? " selected" : ""}>${v}</option>`
  ).join("\n          ");
  return `<select name="${name}" id="${name}">\n          ${options}\n        </select>`;
}

/**
 * Generate the configuration form HTML page
 */
async function buildConfigurePage(): Promise<string> {
  const [distributions, providers, cniValues, gitopsValues] = await Promise.all([
    getSchemaEnumValues("distribution"),
    getSchemaEnumValues("provider"),
    getSchemaEnumValues("cni"),
    getSchemaEnumValues("gitops-engine"),
  ]);

  return `
    ${getWizardStyles()}
    <h2>Create KSail Cluster</h2>
    <form id="${ClusterProviderV1.WIZARD_FORM_NAME}">
      <input type="hidden" name="${ClusterProviderV1.SENDING_STEP_KEY}" value="${STEPS.CONFIGURE}" />
      <input type="hidden" name="${ClusterProviderV1.CLUSTER_TYPE_KEY}" value="ksail" />

      <div class="field">
        <label for="clusterName">Cluster Name</label>
        <div class="field-description">Lowercase alphanumeric, may contain hyphens (DNS-1123)</div>
        <input type="text" name="clusterName" id="clusterName" value="local"
               pattern="[a-z0-9]([a-z0-9-]*[a-z0-9])?" maxlength="63" required />
      </div>

      <div class="field">
        <label for="distribution">Distribution</label>
        <div class="field-description">Kubernetes distribution to use</div>
        ${buildSelect("distribution", distributions)}
      </div>

      <div class="field">
        <label for="provider">Provider</label>
        <div class="field-description">Infrastructure provider for cluster nodes</div>
        ${buildSelect("provider", providers)}
      </div>

      <div class="field">
        <label for="cni">CNI</label>
        <div class="field-description">Container Network Interface plugin</div>
        ${buildSelect("cni", cniValues)}
      </div>

      <div class="field">
        <label for="gitopsEngine">GitOps Engine</label>
        <div class="field-description">GitOps engine for declarative management</div>
        ${buildSelect("gitopsEngine", gitopsValues)}
      </div>

      <div class="field">
        <label for="controlPlanes">Control Planes</label>
        <input type="number" name="controlPlanes" id="controlPlanes" value="1" min="1" max="10" />
      </div>

      <div class="field">
        <label for="workers">Workers</label>
        <input type="number" name="workers" id="workers" value="0" min="0" max="100" />
      </div>

      <div class="actions">
        <button type="button" class="btn-primary" onclick="${ClusterProviderV1.NEXT_PAGE}">
          Create Cluster
        </button>
      </div>
    </form>
  `;
}

/**
 * Generate the creating/progress HTML page
 */
function buildCreatingPage(clusterName: string): string {
  return `
    ${getWizardStyles()}
    <div class="progress">
      <div class="spinner"></div>
      <h2>Creating Cluster "${clusterName}"</h2>
      <p>This may take several minutes. Please do not close this window.</p>
    </div>
  `;
}

/**
 * Generate a result HTML page (success or error)
 */
function buildResultPage(success: boolean, clusterName: string, errorMessage?: string): string {
  if (success) {
    return `
      ${getWizardStyles()}
      <div class="progress">
        <h2>Cluster "${clusterName}" Created</h2>
        <p>Your KSail cluster has been created successfully.</p>
        <p>The cluster context has been added to your kubeconfig.</p>
      </div>
    `;
  }
  return `
    ${getWizardStyles()}
    <div class="progress">
      <h2>Failed to Create Cluster</h2>
      <p style="color: var(--vscode-errorForeground);">${errorMessage ?? "Unknown error"}</p>
    </div>
  `;
}

/**
 * Create the KSail ClusterProvider for the Kubernetes extension's "Create Cluster" wizard.
 */
export function createKSailClusterProvider(
  outputChannel: vscode.OutputChannel,
  onClusterCreated?: () => void
): ClusterProviderV1.ClusterProvider {
  return {
    id: "ksail",
    displayName: "KSail",
    supportedActions: ["create"],

    next(wizard: ClusterProviderV1.Wizard, action: ClusterProviderV1.ClusterProviderAction, message: Record<string, string>): void {
      const sendingStep = message[ClusterProviderV1.SENDING_STEP_KEY];

      if (sendingStep === ClusterProviderV1.SELECT_CLUSTER_TYPE_STEP_ID) {
        // First call — show the configuration form
        const pagePromise = buildConfigurePage();
        wizard.showPage(pagePromise);
        return;
      }

      if (sendingStep === STEPS.CONFIGURE) {
        // User submitted the configuration form — create the cluster
        const clusterName = message.clusterName || "local";
        const distribution = message.distribution;
        const provider = message.provider;
        const cni = message.cni;
        const gitopsEngine = message.gitopsEngine;
        const controlPlanes = parseInt(message.controlPlanes, 10) || 1;
        const workers = parseInt(message.workers, 10) || 0;

        // Show creating page
        wizard.showPage(buildCreatingPage(clusterName));

        // Start creation in the background
        createCluster(
          { name: clusterName, distribution, provider, cni, gitopsEngine, controlPlanes, workers },
          outputChannel
        ).then(() => {
          wizard.showPage(buildResultPage(true, clusterName));
          onClusterCreated?.();
        }).catch((error: unknown) => {
          const errorMsg = error instanceof Error ? error.message : String(error);
          outputChannel.appendLine(`Failed to create cluster: ${errorMsg}`);
          wizard.showPage(buildResultPage(false, clusterName, errorMsg));
        });
        return;
      }
    },
  };
}
