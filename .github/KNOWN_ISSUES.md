# Known Issues

## Agentic Workflows - Create Pull Request Failures

**Status:** Upstream issue - Reported to gh-aw maintainers

**Affected Workflows:**
- Update Docs
- Daily QA
- Daily Progress
- Daily Performance Improver
- Daily Test Improver
- PR Fix
- Any workflow using `create_pull_request` safe-output

**Symptoms:**
Workflows timeout after 15 minutes with the error:
```
Failed to generate patch: spawnSync /bin/sh ENOENT
```

**Root Cause:**
The gh-aw `safeoutputs` MCP server runs in a `node:lts-alpine` container (defined in `.github/workflows/*.lock.yml` files). Alpine Linux uses `/bin/ash` instead of `/bin/sh`. When the safeoutputs server attempts to spawn `/bin/sh` to generate git patches, it fails because the file doesn't exist.

**Technical Details:**
1. Workflow configuration specifies `node:lts-alpine` as the container for the safeoutputs MCP server
2. The safeoutputs MCP server code calls `spawnSync('/bin/sh', ...)` to run git commands
3. Alpine Linux does not have `/bin/sh` by default - only `/bin/ash`
4. This results in ENOENT (Error NO ENTry - file not found)

**Evidence:**
See workflow run logs:
- https://github.com/devantler-tech/ksail/actions/runs/21183264616

Relevant log excerpt:
```
- 🔍 rpc **safeoutputs**→`tools/call` {...}
- 🔍 rpc **safeoutputs**←`resp` ⚠️`calling "tools/call": Failed to generate patch: spawnSync /bin/sh ENOENT`
```

**Workaround:**
Currently, there is no workaround available without modifying the upstream gh-aw tool.

**Upstream Issue:**
- [ ] Report to https://github.com/githubnext/gh-aw/issues

**Proposed Fix (Upstream):**
Modify the gh-aw safeoutputs MCP server to either:
1. Use `node:lts` (Debian-based) instead of `node:lts-alpine` as the base image
2. Add a symlink in the container: `RUN ln -sf /bin/ash /bin/sh`
3. Update the code to spawn `/bin/ash` or detect the shell dynamically

**Temporary Mitigation:**
Until the upstream fix is available:
1. Workflows that don't use `create_pull_request` will continue to work
2. For workflows that need PR creation, consider using manual PR creation or alternative tools
3. Monitor https://github.com/githubnext/gh-aw for updates

**Last Updated:** 2026-01-20
