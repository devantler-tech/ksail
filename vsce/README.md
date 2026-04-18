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

## Documentation

Full user documentation, commands reference, and configuration options are available at **[ksail.devantler.tech/vscode-extension](https://ksail.devantler.tech/vscode-extension/)**.

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

## License

Apache-2.0 - See [LICENSE](LICENSE) for details.
