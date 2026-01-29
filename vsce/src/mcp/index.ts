/**
 * MCP module exports
 *
 * This module provides:
 * - MCP server definition provider for native VSCode integration
 * - Schema client for querying tool schemas dynamically
 */

export {
  KSailMcpServerDefinitionProvider, createConfigChangeListener,
  createKSailConfigWatcher,
  initializeServerProvider
} from "./serverProvider.js";

export {
  clearSchemaCache,
  getEnumValues,
  getPropertyDescription,
  getToolProperties,
  getToolSchema,
  getToolSchemas,
  initializeSchemaClient,
  isPropertyRequired,
  type McpInputSchema,
  type McpPropertySchema,
  type McpTool
} from "./schemaClient.js";

