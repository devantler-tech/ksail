/**
 * KSail VSCode Extension
 *
 * Provides integration with KSail for local Kubernetes development.
 *
 * Architecture:
 * - MCP server provider: Registers KSail with VSCode's native MCP infrastructure
 *   for GitHub Copilot and agent mode integration
 * - Binary execution: Direct CLI execution for extension UI (clusters view, commands)
 */

import * as vscode from "vscode";
import { registerCommands } from "./commands/index.js";
import { isBinaryAvailable } from "./ksail/index.js";
import {
  createConfigChangeListener,
  createKSailConfigWatcher,
  KSailMcpServerDefinitionProvider,
} from "./mcp/index.js";
import { StatusBarManager } from "./ui/statusBar.js";
import { ClustersTreeDataProvider } from "./views/clustersView.js";

// Global extension state
let outputChannel: vscode.OutputChannel;
let statusBarManager: StatusBarManager;
let clustersProvider: ClustersTreeDataProvider;

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

  // Create status bar
  statusBarManager = new StatusBarManager();
  context.subscriptions.push(statusBarManager);

  // Create tree data provider for clusters
  clustersProvider = new ClustersTreeDataProvider(outputChannel);

  // Register tree view
  context.subscriptions.push(
    vscode.window.registerTreeDataProvider("ksailClusters", clustersProvider)
  );

  // Register commands
  registerCommands(context, outputChannel, clustersProvider);

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
      clustersProvider.refresh();
    }),
    configWatcher.onDidDelete(async () => {
      await vscode.commands.executeCommand("setContext", "ksail.hasConfig", false);
      clustersProvider.refresh();
    }),
    configWatcher
  );

  // Initial refresh of clusters
  if (hasKsailYaml) {
    clustersProvider.refresh();
  }

  outputChannel.appendLine("KSail extension activated");
}

/**
 * Extension deactivation
 */
export function deactivate(): void {
  outputChannel?.appendLine("KSail extension deactivating...");
}

/**
 * Get the output channel (for use by other modules)
 */
export function getOutputChannel(): vscode.OutputChannel {
  return outputChannel;
}
