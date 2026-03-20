/**
 * Kubectl Utilities
 *
 * Provides functions for querying Kubernetes cluster state via the
 * Kubernetes extension's KubectlV1 API.
 */

import type { KubectlV1 } from "vscode-kubernetes-tools-api";

/**
 * Invoke a kubectl command via the Kubernetes extension's API.
 */
async function invokeKubectl(
  kubectl: KubectlV1,
  args: string
): Promise<{ stdout: string; stderr: string; exitCode: number }> {
  const result = await kubectl.invokeCommand(args);
  if (!result) {
    return { stdout: "", stderr: "kubectl not configured", exitCode: 1 };
  }
  return { stdout: result.stdout, stderr: result.stderr, exitCode: result.code };
}

/**
 * Get logs for a specific pod
 */
export async function getPodLogs(
  kubectl: KubectlV1,
  namespace: string,
  podName: string,
  tailLines = 100
): Promise<string> {
  const result = await invokeKubectl(
    kubectl,
    `logs ${podName} -n ${namespace} --tail ${tailLines}`
  );

  if (result.exitCode !== 0) {
    return `Failed to get logs: ${result.stderr}`;
  }

  return result.stdout;
}
