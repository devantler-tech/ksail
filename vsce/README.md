# KSail VSCode Extension

A VSCode extension that integrates with KSail via the Model Context Protocol (MCP).

## Features

- **MCP Client**: Connect to the KSail MCP server for tool discovery and execution
- **Clusters View**: View and manage Kubernetes clusters in the sidebar
- **Tools View**: Browse available KSail tools organized by category
- **Status Bar**: Connection status indicator with quick connect/disconnect
- **Command Palette**: Full access to cluster lifecycle operations

## Requirements

- [KSail](https://github.com/devantler-tech/ksail) CLI installed and available in PATH
  - **Important**: Requires KSail v5.0.0 or later for correct MCP tool naming
  - Verify your version: `ksail version`
  - Tool names use format `cluster_create` (without `ksail_` prefix)
- A workspace containing `ksail.yaml` configuration file
- Docker running (for local cluster operations)

### Troubleshooting Installation

If you get errors like "unknown tool cluster_create":

1. **Check KSail version**: Run `ksail version` - must be v5.0.0+
2. **Rebuild if using local build**: `cd /path/to/ksail && go build -o ksail`
3. **Verify binary path**: Check VSCode setting `ksail.binaryPath` points to correct binary
4. **Restart VSCode**: After updating KSail, restart VSCode to use new binary

## Installation

### From VSIX

1. Download the latest `.vsix` file from releases
2. Open VSCode and run `Extensions: Install from VSIX...`
3. Select the downloaded file

### From Marketplace

Search for "KSail" in the VSCode Extensions Marketplace.

## Usage

### Auto-Connect

The extension automatically connects to the KSail MCP server when opening a workspace containing `ksail.yaml`. Disable this in settings:

```json
{
  "ksail.autoConnect": false
}
```

### Manual Connection

1. Open Command Palette (`Cmd+Shift+P`)
2. Run `KSail: Connect to MCP Server`

### Cluster Operations

All cluster operations are available via the Command Palette:

- `KSail: Create Cluster` - Create and start a new cluster
- `KSail: Delete Cluster` - Delete the cluster
- `KSail: Start Cluster` - Start an existing cluster
- `KSail: Stop Cluster` - Stop a running cluster
- `KSail: Cluster Info` - Show cluster information
- `KSail: List Clusters` - List all clusters

### Tree Views

The KSail sidebar contains:

- **Clusters**: Lists available clusters with status indicators
- **Tools**: Browse available MCP tools by category

Right-click on items for context menu actions.

## Extension Settings

| Setting               | Description                           | Default |
| --------------------- | ------------------------------------- | ------- |
| `ksail.binaryPath`    | Path to ksail binary                  | `ksail` |
| `ksail.autoConnect`   | Auto-connect when ksail.yaml is found | `true`  |
| `ksail.showStatusBar` | Show status bar item                  | `true`  |

## Development

### Building

```bash
cd vsce
npm ci
npm run compile
```

### Packaging

```bash
npm run package
npx @vscode/vsce package --out ksail.vsix
```

### Testing locally

1. Open the `vsce` folder in VSCode
2. Press `F5` to launch Extension Development Host
3. Open a folder containing `ksail.yaml`

## Architecture

```
vsce/
├── src/
│   ├── extension.ts      # Entry point
│   ├── mcp/
│   │   └── client.ts     # MCP client implementation
│   ├── views/
│   │   ├── clustersView.ts
│   │   └── toolsView.ts
│   ├── commands/
│   │   └── index.ts      # Command registration
│   └── ui/
│       └── statusBar.ts  # Status bar manager
├── dist/                  # Compiled output
└── package.json          # Extension manifest
```

## License

Apache-2.0 - See [LICENSE](LICENSE) for details.
