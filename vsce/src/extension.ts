/**
 * KSail VSCode Extension
 *
 * Provides integration with KSail for local Kubernetes development.
 *
 * Architecture:
 * - MCP server provider: Registers KSail with VSCode's native MCP infrastructure
 *   for GitHub Copilot and agent mode integration
 * - Kubernetes extension integration: Registers KSail as a Cloud Provider,
 *   Cluster Provider, and Cluster Explorer contributor in the Kubernetes
 *   extension's activity bar
 * - Binary execution: Direct CLI execution for extension UI (commands, status)
 */

import * as vscode from "vscode";
import * as k8s from "vscode-kubernetes-tools-api";
import { registerCommands } from "./commands/index.js";
import { isBinaryAvailable } from "./ksail/index.js";
import {
  createKSailCloudProvider,
  createKSailClusterProvider,
  createKSailNodeUICustomizer,
  disposePodLogChannels,
  showPodLogs,
} from "./kubernetes/index.js";
import {
  createConfigChangeListener,
  createKSailConfigWatcher,
  initializeSchemaClient,
  initializeServerProvider,
  KSailMcpServerDefinitionProvider,
} from "./mcp/index.js";

// Global extension state
let outputChannel: vscode.OutputChannel;

/**
 * Extension activation
 */
export async function activate(
  context: vscode.ExtensionContext
): Promise<void> {
  // Create output channel for logging
  outputChannel = vscode.window.createOutputChannel("KSail");
  context.subscriptions.push(outputChannel);

  outputChannel.appendLine("KSail extension activating...");

  // Initialize schema client with extension version
  const extensionVersion = context.extension.packageJSON.version || "0.1.0";
  initializeSchemaClient(extensionVersion);
  initializeServerProvider(extensionVersion);

  // Register MCP server definition provider with VSCode's native MCP infrastructure
  // This makes KSail tools available to GitHub Copilot and agent mode
  const mcpServerProvider = new KSailMcpServerDefinitionProvider();
  context.subscriptions.push(
    vscode.lm.registerMcpServerDefinitionProvider("ksail", mcpServerProvider)
  );
  outputChannel.appendLine("Registered KSail MCP server with VSCode");

  // Watch for ksail.yaml changes to notify VSCode of server availability
  context.subscriptions.push(createKSailConfigWatcher());
  context.subscriptions.push(createConfigChangeListener());

  // Check if KSail binary is available
  const binaryAvailable = await isBinaryAvailable();
  if (!binaryAvailable) {
    outputChannel.appendLine(
      "Warning: KSail binary not found. Please install KSail or configure ksail.binaryPath."
    );
  }

  // Register with the Kubernetes extension's Cloud Explorer (Clouds view)
  const { cloudProvider, treeDataProvider: cloudTreeProvider } =
    createKSailCloudProvider(outputChannel);

  const cloudExplorerAPI = await k8s.extension.cloudExplorer.v1;
  if (cloudExplorerAPI.available) {
    cloudExplorerAPI.api.registerCloudProvider(cloudProvider);
    outputChannel.appendLine("Registered KSail with Kubernetes Cloud Explorer");
  } else {
    outputChannel.appendLine(
      `Cloud Explorer API not available: ${cloudExplorerAPI.reason}`
    );
  }

  // ── Kubernetes Extension: KubectlV1 API ──
  const kubectlAPI = await k8s.extension.kubectl.v1;
  if (!kubectlAPI.available) {
    outputChannel.appendLine(
      `Kubectl API not available: ${kubectlAPI.reason}`
    );
  }

  // ── Kubernetes Extension: Cluster Explorer (context annotation) ──
  const clusterExplorerAPI = await k8s.extension.clusterExplorer.v1_1;
  let invalidateClusterCache: (() => void) | undefined;
  if (clusterExplorerAPI.available) {
    // Annotate KSail-managed contexts with "(KSail · Running/Stopped)" label
    const { customizer, invalidateCache } = createKSailNodeUICustomizer(outputChannel);
    invalidateClusterCache = invalidateCache;
    clusterExplorerAPI.api.registerNodeUICustomizer(customizer);
    outputChannel.appendLine("Registered KSail context customizer");
  } else {
    outputChannel.appendLine(
      `Cluster Explorer API not available: ${clusterExplorerAPI.reason}`
    );
  }

  // Register with the Kubernetes extension's Cluster Provider ("Create Cluster" wizard)
  const clusterProviderAPI = await k8s.extension.clusterProvider.v1;
  if (clusterProviderAPI.available) {
    const ksailClusterProvider = createKSailClusterProvider(outputChannel, () => {
      // Refresh cloud explorer when a cluster is created via the wizard
      cloudTreeProvider.refresh();
      if (cloudExplorerAPI.available) {
        cloudExplorerAPI.api.refresh();
      }
      if (clusterExplorerAPI.available) {
        clusterExplorerAPI.api.refresh();
      }
    });
    clusterProviderAPI.api.register(ksailClusterProvider);
    outputChannel.appendLine("Registered KSail with Kubernetes Cluster Provider");
    // Debug: list all registered providers to verify registration persists
    const registeredProviders = clusterProviderAPI.api.list();
    outputChannel.appendLine(`Registered cluster providers: ${JSON.stringify(registeredProviders.map(p => p.id))}`);
  } else {
    outputChannel.appendLine(
      `Cluster Provider API not available: ${clusterProviderAPI.reason}`
    );
  }

  // Register commands (cloud explorer context commands + standalone commands)
  registerCommands(context, outputChannel, cloudTreeProvider, cloudExplorerAPI, clusterExplorerAPI.available ? clusterExplorerAPI : undefined, invalidateClusterCache);

  // ── Kubernetes Extension: ConfigurationV1_1 (reactive events) ──
  const configAPI = await k8s.extension.configuration.v1_1;
  if (configAPI.available) {
    // Refresh on context switch
    context.subscriptions.push(
      configAPI.api.onDidChangeContext(() => {
        cloudTreeProvider.refresh();
        if (cloudExplorerAPI.available) {
          cloudExplorerAPI.api.refresh();
        }
        if (clusterExplorerAPI.available) {
          clusterExplorerAPI.api.refresh();
        }
      })
    );

    // Refresh on namespace switch
    context.subscriptions.push(
      configAPI.api.onDidChangeNamespace(() => {
        if (clusterExplorerAPI.available) {
          clusterExplorerAPI.api.refresh();
        }
      })
    );

    // Refresh on kubeconfig path change
    context.subscriptions.push(
      configAPI.api.onDidChangeKubeconfigPath(() => {
        cloudTreeProvider.refresh();
        if (cloudExplorerAPI.available) {
          cloudExplorerAPI.api.refresh();
        }
        if (clusterExplorerAPI.available) {
          clusterExplorerAPI.api.refresh();
        }
      })
    );

    outputChannel.appendLine("Listening for Kubernetes configuration changes");
  } else {
    outputChannel.appendLine(
      `Configuration API not available: ${configAPI.reason}`
    );
  }

  // Register pod logs command (used by ClusterExplorer pod nodes)
  context.subscriptions.push(
    vscode.commands.registerCommand(
      "ksail.status.showPodLogs",
      async (namespace: string, podName: string) => {
        if (kubectlAPI.available) {
          await showPodLogs(kubectlAPI.api, namespace, podName);
        } else {
          vscode.window.showErrorMessage("Kubectl API not available");
        }
      }
    )
  );

  // Set context for when clause (based on ksail.yaml presence in workspace)
  const ksailYaml = await vscode.workspace.findFiles("ksail.yaml", null, 1);
  const hasKsailYaml = ksailYaml.length > 0;
  await vscode.commands.executeCommand(
    "setContext",
    "ksail.hasConfig",
    hasKsailYaml
  );

  // Watch for ksail.yaml creation/deletion
  const configWatcher = vscode.workspace.createFileSystemWatcher("**/ksail.yaml");
  context.subscriptions.push(
    configWatcher.onDidCreate(async () => {
      await vscode.commands.executeCommand("setContext", "ksail.hasConfig", true);
      cloudTreeProvider.refresh();
    }),
    configWatcher.onDidDelete(async () => {
      await vscode.commands.executeCommand("setContext", "ksail.hasConfig", false);
      cloudTreeProvider.refresh();
    }),
    configWatcher
  );

  outputChannel.appendLine("KSail extension activated");
}

/**
 * Extension deactivation
 */
export function deactivate(): void {
  outputChannel?.appendLine("KSail extension deactivating...");
  disposePodLogChannels();
}

/**
 * Get the output channel (for use by other modules)
 */
export function getOutputChannel(): vscode.OutputChannel {
  return outputChannel;
}
