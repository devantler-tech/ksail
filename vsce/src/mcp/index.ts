/**
 * MCP module exports
 *
 * This module provides the MCP server definition provider for native VSCode
 * integration (GitHub Copilot / agent mode). The former MCP schema client was
 * removed in Phase 4.3b: it spoke the wrong stdio framing, queried a tool that
 * does not exist (the consolidated surface is cluster_read/cluster_write), and
 * the consolidated schemas carry no enum values — so every wizard step hung for
 * ~10s before falling back. The wizards now read the static ENUM_CATALOG
 * (src/ksail/enums.ts).
 */

export {
  KSailMcpServerDefinitionProvider, createConfigChangeListener,
  createKSailConfigWatcher,
  initializeServerProvider
} from "./serverProvider.js";

