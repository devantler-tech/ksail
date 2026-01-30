/**
 * KSail Binary Execution
 *
 * Provides direct execution of the KSail CLI binary.
 */

import { spawn } from "child_process";
import * as vscode from "vscode";

/**
 * Result from running a KSail command
 */
export interface KSailResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

/**
 * Get the configured KSail binary path
 */
export function getBinaryPath(): string {
  const config = vscode.workspace.getConfiguration("ksail");
  return config.get<string>("binaryPath", "ksail");
}

/**
 * Run a KSail command and return the result
 *
 * @param args Command arguments
 * @param cwd Working directory (defaults to first workspace folder)
 * @param outputChannel Optional output channel for streaming output
 */
export async function runKsailCommand(
  args: string[],
  cwd?: string,
  outputChannel?: vscode.OutputChannel
): Promise<KSailResult> {
  const binaryPath = getBinaryPath();
  const workingDir = cwd ?? vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;

  if (outputChannel) {
    outputChannel.appendLine(`> ${binaryPath} ${args.join(" ")}`);
  }

  return new Promise((resolve, reject) => {
    const proc = spawn(binaryPath, args, {
      cwd: workingDir,
      env: { ...process.env },
    });

    let stdout = "";
    let stderr = "";

    proc.stdout.on("data", (data: Buffer) => {
      const text = data.toString();
      stdout += text;
      if (outputChannel) {
        outputChannel.append(text);
      }
    });

    proc.stderr.on("data", (data: Buffer) => {
      const text = data.toString();
      stderr += text;
      if (outputChannel) {
        outputChannel.append(text);
      }
    });

    proc.on("error", (error) => {
      reject(new Error(`Failed to execute ${binaryPath}: ${error.message}`));
    });

    proc.on("close", (code) => {
      resolve({
        stdout,
        stderr,
        exitCode: code ?? 0,
      });
    });
  });
}

/**
 * Check if the KSail binary is available
 */
export async function isBinaryAvailable(): Promise<boolean> {
  try {
    const result = await runKsailCommand(["--version"]);
    return result.exitCode === 0;
  } catch {
    return false;
  }
}
