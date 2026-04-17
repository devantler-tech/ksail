# KSail VSCode Extension

A VSCode extension for managing local Kubernetes clusters with KSail. Integrates with the [VS Code Kubernetes extension](https://marketplace.visualstudio.com/items?itemName=ms-kubernetes-tools.vscode-kubernetes-tools) to surface KSail clusters in the Cloud Explorer and Cluster Explorer.

## Features

- **Cloud Explorer Integration**: KSail clusters appear under **KSail** in the Kubernetes extension's Clouds view with status icons and context menus
- **Cluster Explorer Contributor**: KSail-managed kubeconfig contexts are annotated with `(KSail)` and a status label in the Kubernetes extension's Cluster Explorer
- **Cluster Provider Wizard**: HTML-based "Create Cluster" wizard integrated into the Kubernetes extension; enum values fetched live from the KSail MCP schema
- **Interactive Wizards**: Step-by-step configuration for init and create operations
- **Command Palette**: Full access to cluster lifecycle operations (init, create, update, start, stop, switch, backup, restore, delete, connect)
- **Keyboard Shortcuts**: Quick access to common operations
- **MCP Server Provider**: Exposes KSail as an MCP server for AI assistants

## Requirements

- [KSail](https://ksail.devantler.tech/installation/) CLI installed and available in PATH
- Docker running (for local cluster operations)
- [VS Code Kubernetes Tools](https://marketplace.visualstudio.com/items?itemName=ms-kubernetes-tools.vscode-kubernetes-tools) extension (installed automatically as a dependency)

## Installation

### From VSIX

1. Download the latest `.vsix` file from releases
2. Open VSCode and run `Extensions: Install from VSIX...`
3. Select the downloaded file

### From Marketplace

Search for "KSail" in the VSCode Extensions Marketplace, or type `@mcp` in the Extensions view to filter MCP-compatible extensions and find KSail there.

## Usage

### Cluster Operations

All cluster operations are available via the Command Palette (`Cmd+Shift+P` / `Ctrl+Shift+P`):

| Command                           | Description                                                                     | Shortcut        |
|-----------------------------------|---------------------------------------------------------------------------------|-----------------|
| `KSail: Init Cluster`             | Initialize a new ksail.yaml configuration                                       | `Cmd+Shift+K I` |
| `KSail: Create Cluster`           | Create and start a cluster                                                      | `Cmd+Shift+K C` |
| `KSail: Update Cluster`           | Update a running cluster                                                        | -               |
| `KSail: Start Cluster`            | Start an existing cluster                                                       | -               |
| `KSail: Stop Cluster`             | Stop a running cluster                                                          | -               |
| `KSail: Switch Cluster`           | Switch the active kubeconfig context                                            | -               |
| `KSail: Backup Cluster`           | Backup cluster resources                                                        | -               |
| `KSail: Restore Cluster`          | Restore cluster resources from a backup                                         | -               |
| `KSail: Delete Cluster`           | Delete the cluster                                                              | `Cmd+Shift+K D` |
| `KSail: Connect to Cluster (K9s)` | Open K9s terminal UI (embedded in KSail)                                        | -               |
| `KSail: Show KSail Info`          | Show cluster info via KSail CLI                                                 | -               |
| `KSail: Refresh Clusters`         | Refresh the Cloud Explorer and Cluster Explorer views (and clear cached status) | -               |
| `KSail: Show Output`              | Open the KSail output channel                                                   | -               |

Commands are also available via right-click context menus in the Kubernetes extension's Cloud Explorer and Cluster Explorer.

### Interactive Wizards

The **Init** and **Create** commands feature multi-step wizards with:

- Distribution selection (Vanilla/K3s/Talos/VCluster/KWOK)
- Provider selection (Docker/Hetzner/Omni)
- Component configuration (CNI, CSI, GitOps engine, etc.)
- Output path selection for generated files

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

```text
vsce/
├── src/
│   ├── extension.ts          # Entry point, command registration
│   ├── commands/
│   │   ├── index.ts          # Command handlers (command registry)
│   │   └── prompts.ts        # Interactive wizard implementations
│   ├── ksail/
│   │   ├── clusters.ts       # KSail CLI wrapper functions
│   │   ├── binary.ts         # KSail binary discovery and execution
│   │   ├── kubectl.ts        # kubectl wrapper for cluster status queries
│   │   └── index.ts          # Module exports
│   ├── kubernetes/
│   │   ├── cloudProvider.ts          # Cloud Explorer tree provider (KSail clusters in Clouds view)
│   │   ├── clusterExplorerContributor.ts  # Annotates KSail contexts in Cluster Explorer
│   │   ├── clusterProvider.ts        # Create Cluster wizard (HTML-based)
│   │   ├── contextNames.ts           # Shared helpers for parsing kubeconfig context names
│   │   └── index.ts                  # Module exports
│   └── mcp/
│       ├── serverProvider.ts # MCP server definition provider
│       ├── schemaClient.ts   # MCP schema client for KSail
│       └── index.ts          # Module exports
├── dist/                     # Compiled output
└── package.json              # Extension manifest
```

## Documentation

Full user documentation, screenshots, and detailed usage guides are available at **[ksail.devantler.tech/vscode-extension](https://ksail.devantler.tech/vscode-extension/)**.

## License

Apache-2.0 - See [LICENSE](LICENSE) for details.
