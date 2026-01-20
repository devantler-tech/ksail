# Safe Outputs MCP Server Fails with ENOENT on Alpine Container

## Summary
The safeoutputs MCP server fails when attempting to create pull requests because it tries to spawn `/bin/sh` which doesn't exist in the `node:lts-alpine` container.

## Environment
- **gh-aw version:** v0.37.0
- **Container:** `node:lts-alpine` (specified in workflow lock files)
- **Operating System:** GitHub Actions (ubuntu-latest runner)
- **Shell:** Alpine Linux (uses `/bin/ash` not `/bin/sh`)

## Steps to Reproduce
1. Create an agentic workflow with `create_pull_request` safe-output configured
2. Trigger the workflow with changes that would result in a PR
3. Observe the agent create file changes and commit them
4. When the agent calls `create_pull_request`, it fails with ENOENT

## Expected Behavior
The safeoutputs MCP server should successfully:
1. Generate a git patch from the committed changes
2. Create a pull request with the changes
3. Return success to the agent

## Actual Behavior
The safeoutputs MCP server fails with:
```
Failed to generate patch: spawnSync /bin/sh ENOENT
```

This causes the workflow to timeout after 15 minutes as the agent keeps retrying.

## Error Logs
From workflow run: https://github.com/devantler-tech/ksail/actions/runs/21183264616

Gateway log excerpt:
```
- 🔍 rpc **safeoutputs**→`tools/call` {
    "jsonrpc":"2.0",
    "method":"tools/call",
    "params":{
      "arguments":{
        "body":"## Summary...",
        "title":"docs: restore contributing guide to documentation site",
        "branch":"docs/restore-contributing-guide"
      },
      "name":"create_pull_request"
    }
  }
- 🔍 rpc **safeoutputs**←`resp` ⚠️`calling "tools/call": Failed to generate patch: spawnSync /bin/sh ENOENT`
```

## Root Cause
The safeoutputs MCP server is configured to run in a `node:lts-alpine` container (line 378 in update-docs.lock.yml):

```yaml
"safeoutputs": {
  "type": "stdio",
  "container": "node:lts-alpine",
  "entrypoint": "node",
  "entrypointArgs": ["/opt/gh-aw/safeoutputs/mcp-server.cjs"],
  ...
}
```

Alpine Linux uses BusyBox's `ash` shell and does not provide `/bin/sh` by default. When the safeoutputs server attempts to spawn `/bin/sh` to run git commands, it fails with ENOENT (file not found).

## Proposed Solutions

### Option 1: Use Debian-based Node Image (Recommended)
Change the container from `node:lts-alpine` to `node:lts`:

```yaml
"safeoutputs": {
  "type": "stdio",
  "container": "node:lts",  # Changed from node:lts-alpine
  ...
}
```

**Pros:**
- Simple fix
- Debian images include `/bin/sh` by default
- More compatible with Node.js ecosystem

**Cons:**
- Larger image size (~200MB vs ~40MB)

### Option 2: Add Shell Symlink in Alpine Container
Modify the container initialization to create a symlink:

```yaml
"safeoutputs": {
  "type": "stdio",
  "container": "node:lts-alpine",
  "entrypoint": "sh",
  "entrypointArgs": [
    "-c",
    "ln -sf /bin/ash /bin/sh && node /opt/gh-aw/safeoutputs/mcp-server.cjs"
  ],
  ...
}
```

**Pros:**
- Keeps Alpine image (smaller size)
- Minimal change

**Cons:**
- More complex configuration
- May require additional permissions

### Option 3: Update Safeoutputs Code to Use Correct Shell
Modify the safeoutputs MCP server code to:
1. Detect the available shell (`/bin/sh`, `/bin/ash`, `/bin/bash`)
2. Use the detected shell for spawning processes

**Pros:**
- Most robust solution
- Works on any base image

**Cons:**
- Requires code changes
- More complexity

## Impact
This issue affects any workflow using the `create_pull_request` safe-output on Alpine-based containers. Affected workflows will timeout and fail to create PRs.

## Workaround
Currently, there is no workaround without modifying the gh-aw tool itself.

## Additional Context
- This is a common issue when running Node.js applications in Alpine containers
- Similar issues: https://github.com/nodejs/docker-node/issues/380
- Alpine Linux documentation: https://wiki.alpinelinux.org/wiki/Shell

## Suggested Fix Location
The fix should be in the safeoutputs MCP server implementation or in the workflow compilation logic that generates the container configuration.
