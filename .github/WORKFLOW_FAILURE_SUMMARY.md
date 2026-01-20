# Workflow Failure Investigation Summary

## Issue
GitHub Issue #1847 - Agentic Workflow Issues (Update Docs workflow failing)

## Investigation Results

### Root Cause Identified
The Update Docs workflow (and other workflows using `create_pull_request` safe-output) fails with:
```
Failed to generate patch: spawnSync /bin/sh ENOENT
```

### Technical Analysis

**Problem:** The gh-aw safeoutputs MCP server attempts to spawn `/bin/sh` to run git commands, but this shell doesn't exist in Alpine Linux containers.

**Details:**
1. Workflow configuration uses `node:lts-alpine` as the container for safeoutputs MCP server
2. Alpine Linux uses `/bin/ash` (from BusyBox) instead of `/bin/sh`
3. When safeoutputs tries to `spawnSync('/bin/sh', ...)`, it fails with ENOENT
4. This causes workflow to timeout after 15 minutes

**Evidence:**
- Workflow run: https://github.com/devantler-tech/ksail/actions/runs/21183264616
- Gateway logs show: `⚠️calling "tools/call": Failed to generate patch: spawnSync /bin/sh ENOENT`

### Classification
**Upstream Issue** - This is a bug in the gh-aw tool itself, not in the KSail repository configuration.

### Affected Workflows
All workflows using `create_pull_request` safe-output:
- Update Docs
- Daily QA  
- Daily Progress
- Daily Performance Improver
- Daily Test Improver
- PR Fix

### Documentation Created
1. `.github/KNOWN_ISSUES.md` - Documents the issue for KSail users
2. `.github/GH_AW_UPSTREAM_ISSUE.md` - Detailed issue report for gh-aw maintainers

### Proposed Solutions (Upstream)

**Option 1 (Recommended):** Use Debian-based Node image
```yaml
container: "node:lts"  # instead of node:lts-alpine
```

**Option 2:** Add shell symlink
```yaml
entrypoint: "sh"
entrypointArgs: ["-c", "ln -sf /bin/ash /bin/sh && node /opt/gh-aw/safeoutputs/mcp-server.cjs"]
```

**Option 3:** Update safeoutputs code to detect shell dynamically

### Actions Taken
- [x] Investigated workflow failure
- [x] Identified root cause
- [x] Confirmed upstream issue
- [x] Documented findings
- [x] Created upstream issue template
- [x] Prepared issue report for gh-aw repository

### Next Steps
1. Submit issue to https://github.com/githubnext/gh-aw/issues
2. Monitor for upstream fix
3. Update KSail workflows once fix is available

### Workaround
Currently, there is no workaround available without modifying the upstream gh-aw tool.

### Timeline
- **Issue Reported:** 2026-01-20
- **Investigation Completed:** 2026-01-20
- **Upstream Fix:** Pending

## Conclusion
The workflow failures are caused by an upstream bug in gh-aw. The issue has been thoroughly documented and is ready to be reported to the gh-aw maintainers. No changes to the KSail repository can fix this issue - it must be resolved upstream.
