# KSail VSCode Extension

A VSCode extension for managing local Kubernetes clusters with KSail.

## Features

- **Clusters View**: View and manage Kubernetes clusters in the sidebar with provider info
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

### Interactive Wizards

The **Init** and **Create** commands feature multi-step wizards with:

- Distribution selection (Vanilla/K3s/Talos/VCluster)
- Provider selection (Docker/Hetzner)
- Component configuration (CNI, CSI, GitOps engine, etc.)
- Output path selection for generated files

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

| Setting            | Description          | Default |
|--------------------|----------------------|---------|
| `ksail.binaryPath` | Path to ksail binary | `ksail` |

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
│   │   └── index.ts          # Module exports
│   ├── mcp/
│   │   ├── serverProvider.ts # MCP server definition provider
│   │   ├── schemaClient.ts   # MCP schema client for KSail
│   │   └── index.ts          # Module exports
│   └── views/
│       ├── clustersView.ts   # Tree view provider
│       └── index.ts          # Module exports
├── dist/                     # Compiled output
└── package.json              # Extension manifest
```

## License

Apache-2.0 - See [LICENSE](LICENSE) for details.
