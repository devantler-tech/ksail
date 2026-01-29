/**
 * MCP Schema Client
 *
 * Queries the KSail MCP server for tool schemas to enable
 * dynamic prompt generation based on actual CLI capabilities.
 */

import { spawn, type ChildProcess } from "child_process";
import * as vscode from "vscode";

/**
 * JSON-RPC 2.0 request structure
 */
interface JsonRpcRequest {
  jsonrpc: "2.0";
  id: number;
  method: string;
  params?: unknown;
}

/**
 * JSON-RPC 2.0 response structure
 */
interface JsonRpcResponse {
  jsonrpc: "2.0";
  id: number;
  result?: unknown;
  error?: {
    code: number;
    message: string;
    data?: unknown;
  };
}

/**
 * MCP Tool definition from tools/list
 */
export interface McpTool {
  name: string;
  description?: string;
  inputSchema?: McpInputSchema;
}

/**
 * JSON Schema for tool input
 */
export interface McpInputSchema {
  type?: string;
  properties?: Record<string, McpPropertySchema>;
  required?: string[];
  additionalProperties?: boolean;
}

/**
 * Property schema with enum support
 */
export interface McpPropertySchema {
  type?: string | string[];
  description?: string;
  enum?: string[];
  default?: unknown;
  items?: McpPropertySchema;
}

/**
 * Tools list response
 */
interface ToolsListResult {
  tools: McpTool[];
}

/**
 * Cached tool schemas
 */
let cachedTools: McpTool[] | undefined;
let cacheTimestamp = 0;
const CACHE_TTL_MS = 60000; // 1 minute cache

/**
 * Get the KSail binary path from configuration
 */
function getBinaryPath(): string {
  const config = vscode.workspace.getConfiguration("ksail");
  return config.get<string>("binaryPath", "ksail");
}

/**
 * Get the working directory (first workspace folder)
 */
function getCwd(): string | undefined {
  return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
}

/**
 * Query the MCP server for tool schemas
 *
 * Spawns the MCP server, sends tools/list, and returns the result.
 */
async function queryToolsList(): Promise<McpTool[]> {
  const binaryPath = getBinaryPath();
  const cwd = getCwd();

  return new Promise((resolve, reject) => {
    const proc: ChildProcess = spawn(binaryPath, ["mcp"], {
      cwd,
      stdio: ["pipe", "pipe", "pipe"],
    });

    let stdout = "";
    let stderr = "";
    let requestId = 1;
    let initializeId = 0;
    let toolsListId = 0;

    const sendRequest = (method: string, params?: unknown): number => {
      const id = requestId++;
      const request: JsonRpcRequest = {
        jsonrpc: "2.0",
        id,
        method,
        params,
      };
      const message = JSON.stringify(request);
      const content = `Content-Length: ${Buffer.byteLength(message)}\r\n\r\n${message}`;
      proc.stdin?.write(content);
      return id;
    };

    const parseMessages = (data: string): JsonRpcResponse[] => {
      const responses: JsonRpcResponse[] = [];
      let remaining = data;

      while (remaining.length > 0) {
        const headerMatch = remaining.match(/^Content-Length: (\d+)\r\n\r\n/);
        if (!headerMatch) {
          break; // No more complete headers
        }

        const contentLength = parseInt(headerMatch[1], 10);
        const headerLength = headerMatch[0].length;
        const totalLength = headerLength + contentLength;

        if (remaining.length < totalLength) {
          break; // Incomplete message
        }

        const messageContent = remaining.slice(
          headerLength,
          headerLength + contentLength
        );
        remaining = remaining.slice(totalLength);

        try {
          const parsed = JSON.parse(messageContent) as JsonRpcResponse;
          if (parsed.jsonrpc === "2.0") {
            responses.push(parsed);
          }
        } catch {
          // Invalid JSON, skip this message
        }
      }

      return responses;
    };

    const cleanup = (): void => {
      proc.kill();
    };

    // Set timeout
    const timeout = setTimeout(() => {
      cleanup();
      reject(new Error("MCP server query timed out"));
    }, 10000);

    proc.stdout?.on("data", (data: Buffer) => {
      stdout += data.toString();

      const responses = parseMessages(stdout);
      for (const response of responses) {
        if (response.id === initializeId) {
          // Handle initialize response
          if (response.error) {
            clearTimeout(timeout);
            cleanup();
            reject(new Error(response.error.message));
            return;
          }
          // Send tools/list after successful initialization
          toolsListId = sendRequest("tools/list", {});
        } else if (response.id === toolsListId) {
          // Handle tools/list response
          clearTimeout(timeout);
          cleanup();

          if (response.error) {
            reject(new Error(response.error.message));
          } else {
            const result = response.result as ToolsListResult;
            resolve(result.tools || []);
          }
          return;
        }
      }
    });

    proc.stderr?.on("data", (data: Buffer) => {
      stderr += data.toString();
    });

    proc.on("error", (err) => {
      clearTimeout(timeout);
      reject(new Error(`Failed to spawn MCP server: ${err.message}`));
    });

    proc.on("close", (code) => {
      clearTimeout(timeout);
      if (code !== 0 && code !== null) {
        reject(new Error(`MCP server exited with code ${code}: ${stderr}`));
      }
    });

    // Send initialize request
    initializeId = sendRequest("initialize", {
      protocolVersion: "2024-11-05",
      capabilities: {},
      clientInfo: {
        name: "vscode-ksail",
        version: "0.1.0",
      },
    });
  });
}

/**
 * Get all tool schemas (cached)
 */
export async function getToolSchemas(): Promise<McpTool[]> {
  const now = Date.now();
  if (cachedTools && now - cacheTimestamp < CACHE_TTL_MS) {
    return cachedTools;
  }

  try {
    cachedTools = await queryToolsList();
    cacheTimestamp = now;
    return cachedTools;
  } catch (error) {
    // Return cached data if available, even if stale
    if (cachedTools) {
      return cachedTools;
    }
    throw error;
  }
}

/**
 * Clear the tool schema cache
 */
export function clearSchemaCache(): void {
  cachedTools = undefined;
  cacheTimestamp = 0;
}

/**
 * Get a specific tool schema by name
 */
export async function getToolSchema(
  toolName: string
): Promise<McpTool | undefined> {
  const tools = await getToolSchemas();
  return tools.find((t) => t.name === toolName);
}

/**
 * Get enum values for a property in a tool's schema
 *
 * @param toolName - The MCP tool name (e.g., "cluster_init")
 * @param propertyName - The property name (e.g., "distribution")
 * @returns Array of allowed values, or undefined if not an enum
 */
export async function getEnumValues(
  toolName: string,
  propertyName: string
): Promise<string[] | undefined> {
  const tool = await getToolSchema(toolName);
  if (!tool?.inputSchema?.properties) {
    return undefined;
  }

  const propSchema = tool.inputSchema.properties[propertyName];
  return propSchema?.enum;
}

/**
 * Get property description from a tool's schema
 */
export async function getPropertyDescription(
  toolName: string,
  propertyName: string
): Promise<string | undefined> {
  const tool = await getToolSchema(toolName);
  if (!tool?.inputSchema?.properties) {
    return undefined;
  }

  const propSchema = tool.inputSchema.properties[propertyName];
  return propSchema?.description;
}

/**
 * Get all properties for a tool
 */
export async function getToolProperties(
  toolName: string
): Promise<Record<string, McpPropertySchema> | undefined> {
  const tool = await getToolSchema(toolName);
  return tool?.inputSchema?.properties;
}

/**
 * Check if a property is required
 */
export async function isPropertyRequired(
  toolName: string,
  propertyName: string
): Promise<boolean> {
  const tool = await getToolSchema(toolName);
  return tool?.inputSchema?.required?.includes(propertyName) ?? false;
}
