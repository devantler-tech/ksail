/**
 * Command Registration
 *
 * Registers all KSail commands with VS Code.
 * Commands execute the KSail binary directly, not via MCP.
 */

import * as vscode from "vscode";
import {
  createCluster,
  deleteCluster,
  getBinaryPath,
  initCluster,
  listClusters,
  startCluster,
  stopCluster,
} from "../ksail/index.js";
import { ClusterItem, ClustersTreeDataProvider } from "../views/index.js";
import {
  promptClusterSelection,
  promptYesNo,
  runClusterCreateWizard,
  runClusterInitWizard,
} from "./prompts.js";

/**
 * Register all extension commands
 */
export function registerCommands(
  context: vscode.ExtensionContext,
  outputChannel: vscode.OutputChannel,
  clustersProvider: ClustersTreeDataProvider
): void {
  // Refresh command
  context.subscriptions.push(
    vscode.commands.registerCommand("ksail.refresh", () => {
      clustersProvider.refresh();
      vscode.window.showInformationMessage("Refreshed KSail clusters");
    })
  );

  // Cluster list
  context.subscriptions.push(
    vscode.commands.registerCommand("ksail.cluster.list", async () => {
      await executeWithProgress("Listing clusters...", async () => {
        try {
          const clusters = await listClusters(outputChannel);
          if (clusters.length === 0) {
            vscode.window.showInformationMessage("No clusters found");
          } else {
            const message = clusters
              .map((c) => `${c.name} (${c.provider})`)
              .join(", ");
            vscode.window.showInformationMessage(`Clusters: ${message}`);
          }
          clustersProvider.refresh();
        } catch (error) {
          showError("list clusters", error, outputChannel);
        }
      });
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
          clustersProvider.refresh();
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

        const clusterName = options.name || "new-cluster";

        // Add pending cluster to tree view
        clustersProvider.addPendingCluster(clusterName);

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

          vscode.window.showInformationMessage(
            `Cluster "${clusterName}" created successfully`
          );
        } finally {
          // Always remove pending cluster and refresh
          clustersProvider.removePendingCluster(clusterName);
          clustersProvider.refresh();
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
      async (item?: ClusterItem) => {
        try {
          let clusterName = item?.cluster.name;

          // If not from context menu, prompt for cluster selection
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
            clustersProvider.refresh();
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
      async (item?: ClusterItem) => {
        try {
          let clusterName = item?.cluster.name;

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
            clustersProvider.refresh();
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
      async (item?: ClusterItem) => {
        try {
          let clusterName = item?.cluster.name;

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
            clustersProvider.refresh();
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
      async (item?: ClusterItem) => {
        try {
          let clusterName = item?.cluster.name;

          // If not from context menu, check for ksail.yaml or prompt for cluster
          if (!clusterName) {
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
            }
            // If ksail.yaml exists and no cluster selected, use default context
          }

          // K9s requires interactive TTY - use VSCode Terminal API
          const workspaceFolder = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
          const binaryPath = getBinaryPath();
          const terminal = vscode.window.createTerminal({
            name: `KSail: K9s${clusterName ? ` (${clusterName})` : ""}`,
            cwd: workspaceFolder,
          });
          terminal.show();

          // Pass --context flag if specific cluster selected
          if (clusterName) {
            terminal.sendText(`${binaryPath} cluster connect --context ${clusterName}`);
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
