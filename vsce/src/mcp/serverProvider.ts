/**
 * KSail MCP Server Definition Provider
 *
 * Registers the KSail MCP server with VSCode's native MCP infrastructure,
 * making tools available to GitHub Copilot and agent mode.
 */

import * as vscode from "vscode";

/**
 * Extension version (set during activation)
 */
let extensionVersion = "0.1.0"; // fallback default

/**
 * Initialize the server provider with extension version
 */
export function initializeServerProvider(version: string): void {
  extensionVersion = version;
}

/**
 * Event emitter for server definition changes
 */
const onDidChangeMcpServerDefinitionsEmitter =
  new vscode.EventEmitter<void>();

/**
 * Creates the MCP server definition for KSail.
 * Always returns a server definition; sets cwd only if a workspace is available.
 */
function createKSailServerDefinition(): vscode.McpStdioServerDefinition {
  const config = vscode.workspace.getConfiguration("ksail");
  const binaryPath = config.get<string>("binaryPath", "ksail");

  // Create the server definition using the constructor
  const serverDef = new vscode.McpStdioServerDefinition(
    "KSail",           // label
    binaryPath,        // command
    ["mcp"],           // args
    {},                // env
    extensionVersion   // version
  );

  // Set working directory if workspace is available
  const workspaceFolder = vscode.workspace.workspaceFolders?.[0]?.uri;
  if (workspaceFolder) {
    serverDef.cwd = workspaceFolder;
  }

  return serverDef;
}

/**
 * KSail MCP Server Definition Provider
 *
 * Implements the VSCode McpServerDefinitionProvider interface to register
 * the KSail MCP server with VSCode's language model infrastructure.
 */
export class KSailMcpServerDefinitionProvider
  implements vscode.McpServerDefinitionProvider
{
  /**
   * Event fired when the MCP server definitions change.
   * VSCode listens to this to know when to refresh the server list.
   */
  onDidChangeMcpServerDefinitions = onDidChangeMcpServerDefinitionsEmitter.event;

  /**
   * Provides the list of MCP server definitions.
   * Called by VSCode to discover available MCP servers.
   */
  provideMcpServerDefinitions(
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    token: vscode.CancellationToken
  ): vscode.ProviderResult<vscode.McpServerDefinition[]> {
    const definition = createKSailServerDefinition();
    return [definition];
  }

  /**
   * Resolves an MCP server definition before starting the server.
   * Can be used for additional validation or user interaction.
   */
  resolveMcpServerDefinition(
    serverDefinition: vscode.McpServerDefinition,
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    token: vscode.CancellationToken
  ): vscode.ProviderResult<vscode.McpServerDefinition> {
    // Return as-is, no additional resolution needed
    // Future: could add binary existence check or version validation
    return serverDefinition;
  }

  /**
   * Notify VSCode that server definitions have changed.
   * Call this when configuration changes or workspace changes.
   */
  static notifyChange(): void {
    onDidChangeMcpServerDefinitionsEmitter.fire();
  }
}

/**
 * Creates a file watcher for ksail.yaml changes.
 * Notifies VSCode when the workspace gains or loses a KSail configuration.
 */
export function createKSailConfigWatcher(): vscode.Disposable {
  const watcher = vscode.workspace.createFileSystemWatcher("**/ksail.yaml");

  const notify = () => KSailMcpServerDefinitionProvider.notifyChange();

  watcher.onDidCreate(notify);
  watcher.onDidDelete(notify);
  watcher.onDidChange(notify);

  return watcher;
}

/**
 * Creates a configuration change listener.
 * Notifies VSCode when KSail settings change (e.g., binaryPath).
 */
export function createConfigChangeListener(): vscode.Disposable {
  return vscode.workspace.onDidChangeConfiguration((e) => {
    if (e.affectsConfiguration("ksail")) {
      KSailMcpServerDefinitionProvider.notifyChange();
    }
  });
}
