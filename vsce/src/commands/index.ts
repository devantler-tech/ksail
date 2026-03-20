/**
 * Command Registration
 *
 * Registers all KSail commands with VS Code.
 * Commands execute the KSail binary directly, not via MCP.
 * Cluster lifecycle commands work with both Cloud Explorer context targets
 * and standalone command palette invocations.
 */

import * as vscode from "vscode";
import type { API, CloudExplorerV1, ClusterExplorerV1_1 } from "vscode-kubernetes-tools-api";
import {
  createCluster,
  deleteCluster,
  detectDistribution,
  getBinaryPath,
  getContextName,
  initCluster,
  listClusters,
  startCluster,
  stopCluster,
} from "../ksail/index.js";
import type { KSailCloudCluster, KSailCloudTreeDataProvider } from "../kubernetes/index.js";
import {
  promptClusterSelection,
  promptYesNo,
  runClusterCreateWizard,
  runClusterInitWizard,
} from "./prompts.js";

/**
 * Resolve a Cloud Explorer command target to a KSail cluster.
 */
function resolveCloudTarget(
  cloudExplorerAPI: API<CloudExplorerV1>,
  target?: unknown
): KSailCloudCluster | undefined {
  if (!cloudExplorerAPI.available || !target) {
    return undefined;
  }
  const node = cloudExplorerAPI.api.resolveCommandTarget(target);
  if (node && node.nodeType === "resource" && node.cloudName === "KSail") {
    return node.cloudResource as KSailCloudCluster;
  }
  return undefined;
}

/**
 * Register all extension commands
 */
export function registerCommands(
  context: vscode.ExtensionContext,
  outputChannel: vscode.OutputChannel,
  cloudTreeProvider: KSailCloudTreeDataProvider,
  cloudExplorerAPI: API<CloudExplorerV1>,
  clusterExplorerAPI?: API<ClusterExplorerV1_1>
): void {
  // Refresh command
  context.subscriptions.push(
    vscode.commands.registerCommand("ksail.refresh", () => {
      cloudTreeProvider.refresh();
      if (cloudExplorerAPI.available) {
        cloudExplorerAPI.api.refresh();
      }
      if (clusterExplorerAPI?.available) {
        clusterExplorerAPI.api.refresh();
      }
      vscode.window.showInformationMessage("Refreshed KSail clusters");
    })
  );

  // Cluster init (with multi-step wizard)
  context.subscriptions.push(
    vscode.commands.registerCommand("ksail.cluster.init", async () => {
      try {
        // Run multi-step wizard
        const options = await runClusterInitWizard();
        if (!options) { return; }

        // Execute init
        await executeWithProgress("Initializing cluster...", async () => {
          await initCluster(
            {
              name: options.name,
              distribution: options.distribution,
              provider: options.provider,
              cni: options.cni,
              gitopsEngine: options.gitopsEngine,
              outputDir: options.outputPath,
            },
            outputChannel
          );
          vscode.window.showInformationMessage(
            `Cluster "${options.name}" initialized successfully`
          );
          cloudTreeProvider.refresh();
        });
      } catch (error) {
        showError("initialize cluster", error, outputChannel);
      }
    })
  );

  // Cluster create (with multi-step wizard)
  context.subscriptions.push(
    vscode.commands.registerCommand("ksail.cluster.create", async () => {
      try {
        // Run multi-step wizard
        const options = await runClusterCreateWizard();
        if (!options) { return; }

        const clusterName = options.name || "cluster";

        try {
          await vscode.window.withProgress(
            {
              location: vscode.ProgressLocation.Notification,
              title: `Creating cluster "${clusterName}"...`,
              cancellable: false,
            },
            async (progress) => {
              progress.report({ message: "Starting..." });

              await createCluster({
                name: options.name,
                distributionConfigPath: options.distributionConfigPath,
                distribution: options.distribution,
                provider: options.provider,
                cni: options.cni,
                csi: options.csi,
                metricsServer: options.metricsServer,
                certManager: options.certManager,
                policyEngine: options.policyEngine,
                gitopsEngine: options.gitopsEngine,
                controlPlanes: options.controlPlanes,
                workers: options.workers,
              }, outputChannel);
            }
          );

          const successMessage = options.name
            ? `Cluster "${options.name}" created successfully`
            : "Cluster created successfully";
          vscode.window.showInformationMessage(successMessage);
        } finally {
          // Refresh cloud explorer
          cloudTreeProvider.refresh();
          if (cloudExplorerAPI.available) {
            cloudExplorerAPI.api.refresh();
          }
          if (clusterExplorerAPI?.available) {
            clusterExplorerAPI.api.refresh();
          }
        }
      } catch (error) {
        showError("create cluster", error, outputChannel);
      }
    })
  );

  // Cluster delete (with storage prompt)
  context.subscriptions.push(
    vscode.commands.registerCommand(
      "ksail.cluster.delete",
      async (target?: unknown) => {
        try {
          const cloud = resolveCloudTarget(cloudExplorerAPI, target);
          let clusterName = cloud?.name;

          // If not from cloud explorer, prompt for cluster selection
          if (!clusterName) {
            const clusters = await listClusters();
            const selected = await promptClusterSelection(
              clusters,
              "Select cluster to delete"
            );
            if (!selected) {return;}
            clusterName = selected.name;
          }

          // Confirm deletion
          const confirm = await vscode.window.showWarningMessage(
            `Are you sure you want to delete cluster "${clusterName}"?`,
            { modal: true },
            "Delete"
          );
          if (confirm !== "Delete") {return;}

          // Ask about storage
          const deleteStorage = await promptYesNo(
            "Delete storage volumes?",
            "Yes, delete storage",
            "No, keep storage"
          );
          if (deleteStorage === undefined) {return;}

          await executeWithProgress("Deleting cluster...", async () => {
            await deleteCluster(
              {
                name: clusterName,
                deleteStorage,
                force: true,
              },
              outputChannel
            );
            vscode.window.showInformationMessage(
              `Cluster "${clusterName}" deleted successfully`
            );
            cloudTreeProvider.refresh();
            if (cloudExplorerAPI.available) {
              cloudExplorerAPI.api.refresh();
            }
            if (clusterExplorerAPI?.available) {
              clusterExplorerAPI.api.refresh();
            }
          });
        } catch (error) {
          showError("delete cluster", error, outputChannel);
        }
      }
    )
  );

  // Cluster start
  context.subscriptions.push(
    vscode.commands.registerCommand(
      "ksail.cluster.start",
      async (target?: unknown) => {
        try {
          const cloud = resolveCloudTarget(cloudExplorerAPI, target);
          let clusterName = cloud?.name;

          if (!clusterName) {
            const clusters = await listClusters();
            const selected = await promptClusterSelection(
              clusters,
              "Select cluster to start"
            );
            if (!selected) {return;}
            clusterName = selected.name;
          }

          await executeWithProgress("Starting cluster...", async () => {
            await startCluster(clusterName, outputChannel);
            vscode.window.showInformationMessage(
              `Cluster "${clusterName}" started successfully`
            );
            cloudTreeProvider.refresh();
            if (cloudExplorerAPI.available) {
              cloudExplorerAPI.api.refresh();
            }
            if (clusterExplorerAPI?.available) {
              clusterExplorerAPI.api.refresh();
            }
          });
        } catch (error) {
          showError("start cluster", error, outputChannel);
        }
      }
    )
  );

  // Cluster stop
  context.subscriptions.push(
    vscode.commands.registerCommand(
      "ksail.cluster.stop",
      async (target?: unknown) => {
        try {
          const cloud = resolveCloudTarget(cloudExplorerAPI, target);
          let clusterName = cloud?.name;

          if (!clusterName) {
            const clusters = await listClusters();
            const selected = await promptClusterSelection(
              clusters,
              "Select cluster to stop"
            );
            if (!selected) {return;}
            clusterName = selected.name;
          }

          await executeWithProgress("Stopping cluster...", async () => {
            await stopCluster(clusterName, outputChannel);
            vscode.window.showInformationMessage(
              `Cluster "${clusterName}" stopped successfully`
            );
            cloudTreeProvider.refresh();
            if (cloudExplorerAPI.available) {
              cloudExplorerAPI.api.refresh();
            }
            if (clusterExplorerAPI?.available) {
              clusterExplorerAPI.api.refresh();
            }
          });
        } catch (error) {
          showError("stop cluster", error, outputChannel);
        }
      }
    )
  );

  // Cluster connect (K9s)
  context.subscriptions.push(
    vscode.commands.registerCommand(
      "ksail.cluster.connect",
      async (target?: unknown) => {
        try {
          let contextName: string | undefined;
          let clusterName: string | undefined;

          // If from cloud explorer, derive context from cluster info
          const cloud = resolveCloudTarget(cloudExplorerAPI, target);
          if (cloud) {
            clusterName = cloud.name;
            const provider = cloud.provider;
            const distribution = await detectDistribution(clusterName, provider);
            contextName = getContextName(clusterName, distribution);
          } else {
            // Not from cloud explorer - check for ksail.yaml or prompt for cluster
            const ksailYamlExists = await vscode.workspace.findFiles("ksail.yaml", null, 1);
            if (ksailYamlExists.length === 0) {
              // No ksail.yaml and no cluster selected - prompt for cluster
              const clusters = await listClusters();
              if (clusters.length === 0) {
                vscode.window.showErrorMessage("No clusters found to connect to.");
                return;
              }
              const selected = await promptClusterSelection(clusters, "Select cluster to connect to");
              if (!selected) { return; }
              clusterName = selected.name;
              const distribution = await detectDistribution(clusterName, selected.provider);
              contextName = getContextName(clusterName, distribution);
            }
            // If ksail.yaml exists and no cluster selected, CLI will use default context
          }

          // K9s requires interactive TTY - use VSCode Terminal API
          const workspaceFolder = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
          const binaryPath = getBinaryPath();
          const terminal = vscode.window.createTerminal({
            name: `KSail: K9s${clusterName ? ` (${clusterName})` : ""}`,
            cwd: workspaceFolder,
          });
          terminal.show();

          // Pass --context flag with correctly derived context name
          if (contextName) {
            terminal.sendText(`${binaryPath} cluster connect --context ${contextName}`);
          } else {
            terminal.sendText(`${binaryPath} cluster connect`);
          }
        } catch (error) {
          showError("connect to cluster", error, outputChannel);
        }
      }
    )
  );

  // Show output channel
  context.subscriptions.push(
    vscode.commands.registerCommand("ksail.showOutput", () => {
      outputChannel.show();
    })
  );
}

/**
 * Execute an async operation with progress indicator
 */
async function executeWithProgress<T>(
  title: string,
  operation: () => Promise<T>
): Promise<T> {
  return vscode.window.withProgress(
    {
      location: vscode.ProgressLocation.Notification,
      title,
      cancellable: false,
    },
    operation
  );
}

/**
 * Show error message and output
 */
function showError(
  action: string,
  error: unknown,
  outputChannel: vscode.OutputChannel
): void {
  const message = error instanceof Error ? error.message : String(error);
  outputChannel.appendLine(`Error: Failed to ${action}: ${message}`);
  vscode.window.showErrorMessage(`Failed to ${action}: ${message}`);
  outputChannel.show();
}
