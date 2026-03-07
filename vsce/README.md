# KSail VSCode Extension

A VSCode extension for managing local Kubernetes clusters with KSail.

## Features

- **Clusters View**: View and manage Kubernetes clusters in the sidebar with provider info
- **Cluster Status View**: Real-time cluster health, pod summaries, and GitOps reconciliation state
- **Status Bar**: Compact cluster health indicator in the VSCode status bar
- **Interactive Wizards**: Step-by-step configuration for init and create operations
- **Command Palette**: Full access to cluster lifecycle operations
- **Keyboard Shortcuts**: Quick access to common operations
- **MCP Server Provider**: Exposes KSail as an MCP server for AI assistants

## Requirements

- [KSail](https://ksail.devantler.tech/installation/) CLI installed and available in PATH
- Docker running (for local cluster operations)

## Installation

### From VSIX

1. Download the latest `.vsix` file from releases
2. Open VSCode and run `Extensions: Install from VSIX...`
3. Select the downloaded file

### From Marketplace

Search for "KSail" in the VSCode Extensions Marketplace.

## Usage

### Cluster Operations

All cluster operations are available via the Command Palette (`Cmd+Shift+P` / `Ctrl+Shift+P`):

| Command                           | Description                               | Shortcut        |
|-----------------------------------|-------------------------------------------|-----------------|
| `KSail: Init Cluster`             | Initialize a new ksail.yaml configuration | `Cmd+Shift+K I` |
| `KSail: Create Cluster`           | Create and start a cluster                | `Cmd+Shift+K C` |
| `KSail: Delete Cluster`           | Delete the cluster                        | `Cmd+Shift+K D` |
| `KSail: Start Cluster`            | Start an existing cluster                 | -               |
| `KSail: Stop Cluster`             | Stop a running cluster                    | -               |
| `KSail: Connect to Cluster (K9s)` | Open K9s terminal UI (embedded in KSail)  | -               |
| `KSail: Refresh Clusters`         | Refresh the clusters tree view            | -               |
| `KSail: Refresh Cluster Status`   | Manually refresh the cluster status view  | -               |
| `KSail: Show Pod Logs`            | Open pod logs in a VSCode output channel  | -               |
| `KSail: Show Output`              | Open the KSail output channel             | -               |

### Interactive Wizards

The **Init** and **Create** commands feature multi-step wizards with:

- Distribution selection (Vanilla/K3s/Talos/VCluster)
- Provider selection (Docker/Hetzner/Omni)
- Component configuration (CNI, CSI, GitOps engine, etc.)
- Output path selection for generated files

### Cluster Status View

The **Cluster Status** panel in the KSail sidebar refreshes every 10 seconds (configurable) and shows:

- **Health indicator** — ✅ Healthy / ⚠️ Degraded / ❌ Error based on pod states
- **Pods section** — Running/pending/failed counts grouped by namespace; click a failed pod to open its logs
- **GitOps section** — Reconciliation state for Flux or ArgoCD when a GitOps engine is active
- **Status bar** — Compact cluster health badge at the bottom of the VSCode window

When no cluster is running the view shows an informational "No cluster running" message.

### Tree View

The KSail sidebar shows:

- **Clusters**: Lists clusters with name and provider (e.g., `my-cluster - Docker`)
- **Status Indicators**: Visual icons show cluster state
  - ✅ Green checkmark: Running
  - 🚫 Red slash: Stopped
  - 📦 Server icon: Unknown status
- **Smart Context Menus**: Right-click shows relevant actions based on cluster state
  - Running clusters: Stop, Delete, Connect
  - Stopped clusters: Start, Delete, Connect
- **Pending Clusters**: Spinner icon during cluster creation

## Extension Settings

| Setting                        | Description                              | Default |
|--------------------------------|------------------------------------------|---------|
| `ksail.binaryPath`             | Path to ksail binary                     | `ksail` |
| `ksail.statusPollingInterval`  | Cluster status polling interval (seconds)| `10`    |

## Development

### Building

```bash
cd vsce
npm ci
npm run compile
```

### Packaging

```bash
npx @vscode/vsce package --no-dependencies
```

### Testing locally

1. Open the `vsce` folder in VSCode
2. Press `F5` to launch Extension Development Host
3. Test commands from the Command Palette

## Architecture

```
vsce/
├── src/
│   ├── extension.ts          # Entry point, command registration
│   ├── commands/
│   │   ├── index.ts          # Command handlers (command registry)
│   │   └── prompts.ts        # Interactive wizard implementations
│   ├── ksail/
│   │   ├── clusters.ts       # KSail CLI wrapper functions
│   │   ├── binary.ts         # KSail binary discovery and execution
│   │   ├── kubectl.ts        # kubectl helpers (pod listing, GitOps state)
│   │   └── index.ts          # Module exports
│   ├── mcp/
│   │   ├── serverProvider.ts # MCP server definition provider
│   │   ├── schemaClient.ts   # MCP schema client for KSail
│   │   └── index.ts          # Module exports
│   └── views/
│       ├── clustersView.ts       # Clusters tree view provider
│       ├── clusterStatusView.ts  # Cluster status tree view (health, pods, GitOps)
│       ├── statusBar.ts          # Status bar health indicator
│       └── index.ts              # Module exports
├── dist/                     # Compiled output
└── package.json              # Extension manifest
```

## License

Apache-2.0 - See [LICENSE](LICENSE) for details.
