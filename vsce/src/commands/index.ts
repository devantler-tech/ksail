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
  backupCluster,
  clusterInfo,
  createCluster,
  deleteCluster,
  getBinaryPath,
  initCluster,
  listClusters,
  parseClusterName,
  resolveContext,
  restoreCluster,
  startCluster,
  stopCluster,
  switchCluster,
  updateCluster,
} from "../ksail/index.js";
import type { KSailCloudCluster } from "../kubernetes/index.js";
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
 * Resolve a Cluster Explorer command target to a KSail cluster name.
 * Returns the cluster name if the target is a KSail-managed context node.
 */
function resolveClusterExplorerTarget(
  clusterExplorerAPI: API<ClusterExplorerV1_1> | undefined,
  target?: unknown
): string | undefined {
  if (!clusterExplorerAPI?.available || !target) {
    return undefined;
  }
  const node = clusterExplorerAPI.api.resolveCommandTarget(target);
  if (!node || node.nodeType !== "context") {
    return undefined;
  }
  return parseClusterName(node.name);
}

/**
 * Dependencies for command registration.
 *
 * `context` is retained so command registrations land in `context.subscriptions`.
 * `refreshAllViews` is the single shared, cache-invalidating refresh function
 * (built once in extension.ts) — there is no second copy here.
 */
export interface CommandDeps {
  context: vscode.ExtensionContext;
  outputChannel: vscode.OutputChannel;
  cloudExplorerAPI: API<CloudExplorerV1>;
  clusterExplorerAPI?: API<ClusterExplorerV1_1>;
  /** Invalidate cached status and refresh all explorer views. */
  refreshAllViews: () => void;
}

/**
 * Register all extension commands
 */
export function registerCommands(deps: CommandDeps): void {
  const { context, outputChannel, cloudExplorerAPI, clusterExplorerAPI, refreshAllViews } = deps;

  /**
   * Resolve a cluster name from a command target (Cloud Explorer or Cluster Explorer)
   * or prompt the user to select one.
   * Returns undefined if the user cancels the selection.
   */
  async function resolveClusterNameOrPrompt(
    target: unknown | undefined,
    promptMessage: string
  ): Promise<string | undefined> {
    const cloud = resolveCloudTarget(cloudExplorerAPI, target);
    const clusterName = cloud?.name ?? resolveClusterExplorerTarget(clusterExplorerAPI, target);
    if (clusterName) {
      return clusterName;
    }
    const clusters = await listClusters();
    const selected = await promptClusterSelection(clusters, promptMessage);
    return selected?.name;
  }

  /**
   * Resolve the kubeconfig context name for a cluster.
   *
   * Prefers a distribution already known (e.g. from a Cloud Explorer target);
   * otherwise looks it up in `cluster list` output. `resolveContext` builds the
   * context name from the distribution (no `docker ps` sniffing).
   */
  async function resolveContextForCluster(
    clusterName: string,
    knownDistribution?: string
  ): Promise<string> {
    let distribution = knownDistribution;
    if (!distribution) {
      const clusters = await listClusters();
      distribution = clusters.find((c) => c.name === clusterName)?.distribution;
    }
    return resolveContext(clusterName, distribution);
  }

  /**
   * Resolve a cluster's provider, falling back to a `cluster list` lookup when
   * it is not already known from the command target.
   */
  async function resolveProviderForCluster(
    clusterName: string,
    knownProvider?: string
  ): Promise<string | undefined> {
    if (knownProvider) {
      return knownProvider;
    }
    const clusters = await listClusters();
    return clusters.find((c) => c.name === clusterName)?.provider;
  }

  // Refresh command
  context.subscriptions.push(
    vscode.commands.registerCommand("ksail.refresh", () => {
      refreshAllViews();
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
          refreshAllViews();
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
          refreshAllViews();
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
          const clusterName = await resolveClusterNameOrPrompt(target, "Select cluster to delete");
          if (!clusterName) {return;}

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
            refreshAllViews();
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
          const clusterName = await resolveClusterNameOrPrompt(target, "Select cluster to start");
          if (!clusterName) {return;}

          await executeWithProgress("Starting cluster...", async () => {
            await startCluster(clusterName, outputChannel);
            vscode.window.showInformationMessage(
              `Cluster "${clusterName}" started successfully`
            );
            refreshAllViews();
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
          const clusterName = await resolveClusterNameOrPrompt(target, "Select cluster to stop");
          if (!clusterName) {return;}

          await executeWithProgress("Stopping cluster...", async () => {
            await stopCluster(clusterName, outputChannel);
            vscode.window.showInformationMessage(
              `Cluster "${clusterName}" stopped successfully`
            );
            refreshAllViews();
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

          // If from cloud explorer, build context from the cluster's distribution
          const cloud = resolveCloudTarget(cloudExplorerAPI, target);
          if (cloud) {
            clusterName = cloud.name;
            contextName = await resolveContextForCluster(clusterName, cloud.distribution);
          }

          // If from cluster explorer, resolve distribution from cluster list
          if (!clusterName) {
            const clusterExplorerName = resolveClusterExplorerTarget(clusterExplorerAPI, target);
            if (clusterExplorerName) {
              clusterName = clusterExplorerName;
              contextName = await resolveContextForCluster(clusterName);
            }
          }

          if (!clusterName) {
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
              contextName = resolveContext(clusterName, selected.distribution);
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

  // Reusable output channel for cluster info display
  const clusterInfoChannel = vscode.window.createOutputChannel("KSail: Cluster Info");
  context.subscriptions.push(clusterInfoChannel);

  // Cluster info
  context.subscriptions.push(
    vscode.commands.registerCommand(
      "ksail.cluster.info",
      async (target?: unknown) => {
        try {
          const cloud = resolveCloudTarget(cloudExplorerAPI, target);
          let clusterName = cloud?.name ?? resolveClusterExplorerTarget(clusterExplorerAPI, target);
          let provider = cloud?.provider;

          if (!clusterName) {
            const clusters = await listClusters();
            const selected = await promptClusterSelection(
              clusters,
              "Select cluster to inspect"
            );
            if (!selected) {return;}
            clusterName = selected.name;
            provider = selected.provider;
          }

          await executeWithProgress("Getting cluster info...", async () => {
            const resolvedProvider = await resolveProviderForCluster(clusterName, provider);
            const info = await clusterInfo(clusterName, resolvedProvider, outputChannel);
            clusterInfoChannel.clear();
            clusterInfoChannel.appendLine(`── Cluster: ${clusterName} ──`);
            clusterInfoChannel.appendLine(info);
            clusterInfoChannel.show();
          });
        } catch (error) {
          showError("get cluster info", error, outputChannel);
        }
      }
    )
  );

  // Cluster update
  context.subscriptions.push(
    vscode.commands.registerCommand(
      "ksail.cluster.update",
      async (target?: unknown) => {
        try {
          const clusterName = await resolveClusterNameOrPrompt(target, "Select cluster to update");
          if (!clusterName) {return;}

          await executeWithProgress("Updating cluster...", async () => {
            await updateCluster(clusterName, outputChannel);
            vscode.window.showInformationMessage(
              `Cluster "${clusterName}" updated successfully`
            );
            refreshAllViews();
          });
        } catch (error) {
          showError("update cluster", error, outputChannel);
        }
      }
    )
  );

  // Cluster backup
  context.subscriptions.push(
    vscode.commands.registerCommand(
      "ksail.cluster.backup",
      async (target?: unknown) => {
        try {
          const clusterName = await resolveClusterNameOrPrompt(target, "Select cluster to backup");
          if (!clusterName) {return;}

          const saveUri = await vscode.window.showSaveDialog({
            defaultUri: vscode.Uri.file(`${clusterName}-backup.tar.gz`),
            filters: { "Backup archives": ["tar.gz"] },
            title: "Save cluster backup",
          });
          if (!saveUri) {return;}

          await executeWithProgress("Backing up cluster...", async () => {
            await backupCluster(saveUri.fsPath, outputChannel);
            vscode.window.showInformationMessage(
              `Cluster "${clusterName}" backed up to ${saveUri.fsPath}`
            );
          });
        } catch (error) {
          showError("backup cluster", error, outputChannel);
        }
      }
    )
  );

  // Cluster restore
  context.subscriptions.push(
    vscode.commands.registerCommand(
      "ksail.cluster.restore",
      async (target?: unknown) => {
        try {
          const clusterName = await resolveClusterNameOrPrompt(target, "Select cluster to restore");
          if (!clusterName) {return;}

          // Confirm restore
          const confirm = await vscode.window.showWarningMessage(
            `Are you sure you want to restore cluster "${clusterName}"? This will overwrite current state.`,
            { modal: true },
            "Restore"
          );
          if (confirm !== "Restore") {return;}

          const openUris = await vscode.window.showOpenDialog({
            canSelectMany: false,
            filters: { "Backup archives": ["tar.gz"] },
            title: "Select backup archive to restore",
          });
          if (!openUris || openUris.length === 0) {return;}

          await executeWithProgress("Restoring cluster...", async () => {
            await restoreCluster(openUris[0].fsPath, outputChannel);
            vscode.window.showInformationMessage(
              `Cluster "${clusterName}" restored successfully`
            );
            refreshAllViews();
          });
        } catch (error) {
          showError("restore cluster", error, outputChannel);
        }
      }
    )
  );

  // Cluster switch
  context.subscriptions.push(
    vscode.commands.registerCommand(
      "ksail.cluster.switch",
      async (target?: unknown) => {
        try {
          const clusterName = await resolveClusterNameOrPrompt(target, "Select cluster to switch to");
          if (!clusterName) {return;}

          await executeWithProgress("Switching cluster...", async () => {
            await switchCluster(clusterName, outputChannel);
            vscode.window.showInformationMessage(
              `Switched to cluster "${clusterName}"`
            );
            refreshAllViews();
          });
        } catch (error) {
          showError("switch cluster", error, outputChannel);
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
