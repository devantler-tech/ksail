/**
 * Kubectl Utilities
 *
 * Provides functions for querying Kubernetes cluster state via kubectl.
 */

import { spawn } from "child_process";

/**
 * Pod phase as reported by kubectl
 */
export type PodPhase = "Running" | "Pending" | "Failed" | "Succeeded" | "Unknown";

/**
 * Pod information from kubectl
 */
export interface PodInfo {
  name: string;
  namespace: string;
  phase: PodPhase;
  ready: string;
  restarts: number;
}

/**
 * Pod summary counts per namespace
 */
export interface NamespacePodSummary {
  namespace: string;
  running: number;
  pending: number;
  failed: number;
  succeeded: number;
  total: number;
}

/**
 * GitOps reconciliation status for a resource
 */
export interface GitOpsStatus {
  kind: string;
  name: string;
  namespace: string;
  ready: string;
  status: string;
}

/**
 * Cluster health status
 */
export type ClusterHealth = "Healthy" | "Degraded" | "Error" | "Unknown";

/**
 * Complete cluster status snapshot
 */
export interface ClusterStatusSnapshot {
  health: ClusterHealth;
  podSummaries: NamespacePodSummary[];
  pods: PodInfo[];
  gitopsStatuses: GitOpsStatus[];
  gitopsEngine: string | undefined;
  error: string | undefined;
}

/**
 * Run a command and capture output
 */
function runCommand(
  command: string,
  args: string[]
): Promise<{ stdout: string; stderr: string; exitCode: number }> {
  return new Promise((resolve) => {
    const proc = spawn(command, args);

    let stdout = "";
    let stderr = "";

    proc.stdout.on("data", (data: Buffer) => {
      stdout += data.toString();
    });

    proc.stderr.on("data", (data: Buffer) => {
      stderr += data.toString();
    });

    proc.on("close", (code) => {
      resolve({ stdout, stderr, exitCode: code ?? 1 });
    });

    proc.on("error", (error) => {
      resolve({ stdout, stderr: stderr || error.message, exitCode: 1 });
    });
  });
}

/**
 * Get all pods across all namespaces
 */
export async function getAllPods(): Promise<PodInfo[]> {
  const result = await runCommand("kubectl", [
    "get", "pods", "--all-namespaces",
    "-o", "jsonpath={range .items[*]}{.metadata.namespace}{\"\\t\"}{.metadata.name}{\"\\t\"}{.status.phase}{\"\\t\"}{range .status.containerStatuses[*]}{.ready}{\" \"}{end}{\"\\t\"}{range .status.containerStatuses[*]}{.restartCount}{\" \"}{end}{\"\\n\"}{end}",
  ]);

  if (result.exitCode !== 0) {
    return [];
  }

  const pods: PodInfo[] = [];
  const lines = result.stdout.trim().split("\n").filter(Boolean);

  for (const line of lines) {
    const parts = line.split("\t");
    if (parts.length < 3) {
      continue;
    }

    const [namespace, name, phase, readyStr, restartsStr] = parts;
    const readyParts = (readyStr || "").trim().split(" ").filter(Boolean);
    const readyCount = readyParts.filter((r) => r === "true").length;
    const totalContainers = readyParts.length || 1;
    const restartParts = (restartsStr || "").trim().split(" ").filter(Boolean);
    const totalRestarts = restartParts.reduce((sum, r) => sum + (parseInt(r, 10) || 0), 0);

    pods.push({
      name,
      namespace,
      phase: phase as PodPhase,
      ready: `${readyCount}/${totalContainers}`,
      restarts: totalRestarts,
    });
  }

  return pods;
}

/**
 * Summarize pods by namespace
 */
export function summarizePodsByNamespace(pods: PodInfo[]): NamespacePodSummary[] {
  const byNamespace = new Map<string, NamespacePodSummary>();

  for (const pod of pods) {
    let summary = byNamespace.get(pod.namespace);
    if (!summary) {
      summary = {
        namespace: pod.namespace,
        running: 0,
        pending: 0,
        failed: 0,
        succeeded: 0,
        total: 0,
      };
      byNamespace.set(pod.namespace, summary);
    }

    summary.total++;
    switch (pod.phase) {
      case "Running":
        summary.running++;
        break;
      case "Pending":
        summary.pending++;
        break;
      case "Failed":
        summary.failed++;
        break;
      case "Succeeded":
        summary.succeeded++;
        break;
    }
  }

  return Array.from(byNamespace.values()).sort((a, b) =>
    a.namespace.localeCompare(b.namespace)
  );
}

/**
 * Detect which GitOps engine is running in the cluster
 */
export async function detectGitOpsEngine(): Promise<string | undefined> {
  // Check for Flux
  const fluxResult = await runCommand("kubectl", [
    "get", "namespace", "flux-system",
    "--no-headers", "--ignore-not-found",
  ]);
  if (fluxResult.exitCode === 0 && fluxResult.stdout.trim()) {
    return "Flux";
  }

  // Check for ArgoCD
  const argoResult = await runCommand("kubectl", [
    "get", "namespace", "argocd",
    "--no-headers", "--ignore-not-found",
  ]);
  if (argoResult.exitCode === 0 && argoResult.stdout.trim()) {
    return "ArgoCD";
  }

  return undefined;
}

/**
 * Get Flux reconciliation statuses
 */
async function getFluxStatuses(): Promise<GitOpsStatus[]> {
  const result = await runCommand("kubectl", [
    "get", "kustomizations.kustomize.toolkit.fluxcd.io",
    "--all-namespaces",
    "-o", "jsonpath={range .items[*]}{.kind}{\"\\t\"}{.metadata.name}{\"\\t\"}{.metadata.namespace}{\"\\t\"}{.status.conditions[?(@.type==\"Ready\")].status}{\"\\t\"}{.status.conditions[?(@.type==\"Ready\")].message}{\"\\n\"}{end}",
  ]);

  if (result.exitCode !== 0) {
    return [];
  }

  return parseGitOpsOutput(result.stdout, "Kustomization");
}

/**
 * Get ArgoCD application statuses
 */
async function getArgoCDStatuses(): Promise<GitOpsStatus[]> {
  const result = await runCommand("kubectl", [
    "get", "applications.argoproj.io",
    "--all-namespaces",
    "-o", "jsonpath={range .items[*]}{.kind}{\"\\t\"}{.metadata.name}{\"\\t\"}{.metadata.namespace}{\"\\t\"}{.status.health.status}{\"\\t\"}{.status.sync.status}{\"\\n\"}{end}",
  ]);

  if (result.exitCode !== 0) {
    return [];
  }

  return parseGitOpsOutput(result.stdout, "Application");
}

/**
 * Parse GitOps command output into statuses
 */
function parseGitOpsOutput(output: string, defaultKind: string): GitOpsStatus[] {
  const statuses: GitOpsStatus[] = [];
  const lines = output.trim().split("\n").filter(Boolean);

  for (const line of lines) {
    const parts = line.split("\t");
    if (parts.length < 4) {
      continue;
    }

    const [kind, name, namespace, ready, status] = parts;
    statuses.push({
      kind: kind || defaultKind,
      name,
      namespace,
      ready,
      status: status || ready,
    });
  }

  return statuses;
}

/**
 * Determine overall cluster health from pod and GitOps data
 */
export function determineClusterHealth(
  pods: PodInfo[],
  gitopsStatuses: GitOpsStatus[],
  gitopsEngine: string | undefined
): ClusterHealth {
  const failedPods = pods.filter((p) => p.phase === "Failed");
  if (failedPods.length > 0) {
    return "Error";
  }

  const pendingPods = pods.filter((p) => p.phase === "Pending");
  if (pendingPods.length > 0) {
    return "Degraded";
  }

  if (gitopsEngine) {
    const failedReconciliations = gitopsStatuses.filter(
      (s) => s.ready === "False" || s.ready === "Degraded"
    );
    if (failedReconciliations.length > 0) {
      return "Degraded";
    }
  }

  if (pods.length === 0) {
    return "Unknown";
  }

  return "Healthy";
}

/**
 * Fetch a complete cluster status snapshot
 */
export async function fetchClusterStatus(): Promise<ClusterStatusSnapshot> {
  try {
    const [pods, gitopsEngine] = await Promise.all([
      getAllPods(),
      detectGitOpsEngine(),
    ]);

    let gitopsStatuses: GitOpsStatus[] = [];
    if (gitopsEngine === "Flux") {
      gitopsStatuses = await getFluxStatuses();
    } else if (gitopsEngine === "ArgoCD") {
      gitopsStatuses = await getArgoCDStatuses();
    }

    const podSummaries = summarizePodsByNamespace(pods);
    const health = determineClusterHealth(pods, gitopsStatuses, gitopsEngine);

    return {
      health,
      podSummaries,
      pods,
      gitopsStatuses,
      gitopsEngine,
      error: undefined,
    };
  } catch (error) {
    return {
      health: "Unknown",
      podSummaries: [],
      pods: [],
      gitopsStatuses: [],
      gitopsEngine: undefined,
      error: error instanceof Error ? error.message : String(error),
    };
  }
}

/**
 * Get logs for a specific pod
 */
export async function getPodLogs(
  namespace: string,
  podName: string,
  tailLines = 100
): Promise<string> {
  const result = await runCommand("kubectl", [
    "logs", podName,
    "-n", namespace,
    "--tail", tailLines.toString(),
  ]);

  if (result.exitCode !== 0) {
    return `Failed to get logs: ${result.stderr}`;
  }

  return result.stdout;
}
