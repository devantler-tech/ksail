/**
 * Command Registration
 *
 * Registers all KSail commands with VS Code.
 * Commands execute the KSail binary directly, not via MCP.
 */

import * as vscode from "vscode";
import {
  connectCluster,
  createCluster,
  deleteCluster,
  getClusterInfo,
  initCluster,
  listClusters,
  startCluster,
  stopCluster,
} from "../ksail/index.js";
import { ClusterItem, ClustersTreeDataProvider } from "../views/index.js";
import {
  promptAdvancedOptions,
  promptClusterName,
  promptClusterSelection,
  promptCNI,
  promptDistribution,
  promptProvider,
  promptYesNo,
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
              .map((c) => `${c.name} (${c.status})`)
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

  // Cluster init (with wizard)
  context.subscriptions.push(
    vscode.commands.registerCommand("ksail.cluster.init", async () => {
      try {
        // Step 1: Cluster name
        const name = await promptClusterName("local");
        if (!name) {return;}

        // Step 2: Distribution
        const distribution = await promptDistribution();
        if (!distribution) {return;}

        // Step 3: Provider
        const provider = await promptProvider();
        if (!provider) {return;}

        // Step 4: CNI
        const cni = await promptCNI();
        if (!cni) {return;}

        // Step 5: Advanced options
        const advanced = await promptAdvancedOptions();
        if (advanced === undefined) {return;}

        // Execute init
        await executeWithProgress("Initializing cluster...", async () => {
          await initCluster(
            {
              name,
              distribution,
              provider,
              cni,
              ...advanced,
            },
            outputChannel
          );
          vscode.window.showInformationMessage(
            `Cluster "${name}" initialized successfully`
          );
          clustersProvider.refresh();
        });
      } catch (error) {
        showError("initialize cluster", error, outputChannel);
      }
    })
  );

  // Cluster create
  context.subscriptions.push(
    vscode.commands.registerCommand("ksail.cluster.create", async () => {
      try {
        // Optional: Prompt for name
        const name = await promptClusterName();

        await executeWithProgress("Creating cluster...", async () => {
          await createCluster({ name }, outputChannel);
          vscode.window.showInformationMessage(
            name
              ? `Cluster "${name}" created successfully`
              : "Cluster created successfully"
          );
          clustersProvider.refresh();
        });
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

  // Cluster info (uses ksail.yaml in workspace)
  context.subscriptions.push(
    vscode.commands.registerCommand(
      "ksail.cluster.info",
      async () => {
        try {
          await executeWithProgress("Getting cluster info...", async () => {
            const info = await getClusterInfo(outputChannel);
            outputChannel.appendLine("\n=== Cluster Info ===");
            outputChannel.appendLine(info);
            outputChannel.show();
          });
        } catch (error) {
          showError("get cluster info", error, outputChannel);
        }
      }
    )
  );

  // Cluster connect (K9s) - uses ksail.yaml in workspace
  context.subscriptions.push(
    vscode.commands.registerCommand(
      "ksail.cluster.connect",
      async () => {
        try {
          // Note: This spawns K9s which is interactive
          await connectCluster(outputChannel);
          vscode.window.showInformationMessage(
            "Connecting to cluster with K9s..."
          );
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
