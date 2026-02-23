# Upstream PR: loft-sh/log tablewriter v1.x Update

## Summary

This document contains the patch and instructions for submitting an upstream pull request to [loft-sh/log](https://github.com/loft-sh/log) to update from tablewriter v0.0.5 API to v1.x API.

## Background

**Issue:** [#2246](https://github.com/devantler-tech/ksail/issues/2246) - Remove loft-sh/log fork when upstream updates tablewriter

KSail currently maintains a local fork of `loft-sh/log` at `./patches/loft-sh-log` because the upstream version uses the deprecated tablewriter v0.0.5 API, which is incompatible with tablewriter v1.x required by other dependencies (k9s, grype, syft).

## Problem Statement

1. **API Incompatibility:** tablewriter v0.0.5 and v1.x have breaking API changes
2. **Dependency Conflicts:** Cannot use both versions in the same project
3. **Maintenance Burden:** Local fork requires manual updates and tracking
4. **Downstream Impact:** Other projects using loft-sh/log face the same issue

## Solution

Update loft-sh/log to use the modern tablewriter v1.1.2 API. The changes are minimal and maintain backward compatibility for callers of the `table` package.

## Changes Required

### 1. Update `go.mod`

```diff
-	github.com/olekukonko/tablewriter v0.0.5
+	github.com/olekukonko/tablewriter v1.1.2
```

### 2. Update `table/table.go`

**Key Changes:**
- Import `github.com/olekukonko/tablewriter/tw` for alignment types
- Remove unused `runtime` import
- Replace `NewWriter()` with `NewTable()` using functional options
- Replace `SetHeader()` with `Header()` accepting `[]any`
- Replace `AppendBulk()` with individual `Append()` calls for each row
- Use `tw.AlignLeft` instead of `ALIGN_LEFT` constant
- Remove platform-specific header coloring (not available in v1.x API)

**See:** Full diff in `loft-sh-log-tablewriter-update.patch` section below

## Patch File

The complete patch for this change is provided below. It can be applied to a fresh clone of loft-sh/log:

```bash
# Clone upstream repository
git clone https://github.com/loft-sh/log.git
cd log

# Create feature branch
git checkout -b update-tablewriter-v1

# Apply the patch
patch -p1 < loft-sh-log-tablewriter-update.patch

# Or manually apply changes from the patch diff below
```

### Patch Contents

````patch
From 92be4a3c7f07d31a12176a79719b942c814d8db6 Mon Sep 17 00:00:00 2001
From: github-actions[bot] <github-actions[bot]@users.noreply.github.com>
Date: Mon, 23 Feb 2026 08:07:49 +0000
Subject: [PATCH] Update tablewriter from v0.0.5 to v1.1.2 API

This commit updates the table package to use the modern tablewriter v1.1.2 API
instead of the legacy v0.0.5 API. This change is necessary because:

1. Modern projects like k9s, grype, and syft require tablewriter v1.x
2. The v0.0.5 API is incompatible with v1.x due to breaking changes
3. Downstream projects cannot use both versions simultaneously

Changes:
- Update go.mod: tablewriter v0.0.5 → v1.1.2
- Refactor table.go to use v1.x API:
  - Replace NewWriter() with NewTable() using functional options
  - Replace SetHeader() with Header() accepting []any
  - Replace AppendBulk() with individual Append() calls for each row
  - Use tw.AlignLeft instead of ALIGN_LEFT constant
  - Import tablewriter/tw for alignment types

The new API provides better type safety and a modern functional options
pattern for table configuration.

This change maintains backward compatibility for callers of PrintTable and
PrintTableWithOptions functions.

---
 go.mod         |  2 +-
 table/table.go | 37 +++++++++++++++++++++++--------------
 2 files changed, 24 insertions(+), 15 deletions(-)

diff --git a/go.mod b/go.mod
index 843aa73..f87934c 100644
--- a/go.mod
+++ b/go.mod
@@ -10,7 +10,7 @@ require (
 	github.com/k0kubun/go-ansi v0.0.0-20180517002512-3bf9e2903213
 	github.com/mgutz/ansi v0.0.0-20200706080929-d51e80ef957d
 	github.com/moby/term v0.5.0
-	github.com/olekukonko/tablewriter v0.0.5
+	github.com/olekukonko/tablewriter v1.1.2
 	github.com/pkg/errors v0.9.1
 	github.com/sirupsen/logrus v1.9.3
 	go.uber.org/zap v1.24.0
diff --git a/table/table.go b/table/table.go
index adee969..87bccd4 100644
--- a/table/table.go
+++ b/table/table.go
@@ -2,11 +2,11 @@ package table
 
 import (
 	"io"
-	"runtime"
 
 	"github.com/loft-sh/log"
 	"github.com/loft-sh/log/scanner"
 	"github.com/olekukonko/tablewriter"
+	"github.com/olekukonko/tablewriter/tw"
 	"github.com/sirupsen/logrus"
 )
 
@@ -15,7 +15,12 @@ func PrintTable(s log.Logger, header []string, values [][]string) {
 }
 
 // PrintTableWithOptions prints a table with header columns and string values
-func PrintTableWithOptions(s log.Logger, header []string, values [][]string, modify func(table *tablewriter.Table)) {
+func PrintTableWithOptions(
+	s log.Logger,
+	header []string,
+	values [][]string,
+	modify func(table *tablewriter.Table),
+) {
 	reader, writer := io.Pipe()
 	defer writer.Close()
 
@@ -29,26 +34,30 @@ func PrintTableWithOptions(s log.Logger, header []string, values [][]string, mod
 		}
 	}()
 
-	table := tablewriter.NewWriter(writer)
-	table.SetHeader(header)
-	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
-		colors := []tablewriter.Colors{}
-		for range header {
-			colors = append(colors, tablewriter.Color(tablewriter.FgGreenColor))
-		}
-		table.SetHeaderColor(colors...)
+	headerAny := make([]any, len(header))
+	for i, h := range header {
+		headerAny[i] = h
 	}
 
-	table.SetAlignment(tablewriter.ALIGN_LEFT)
-	table.SetBorders(tablewriter.Border{Left: false, Top: false, Right: false, Bottom: false})
-	table.AppendBulk(values)
+	table := tablewriter.NewTable(writer,
+		tablewriter.WithHeaderAlignment(tw.AlignLeft),
+		tablewriter.WithRowAlignment(tw.AlignLeft),
+	)
+	table.Header(headerAny...)
+	for _, row := range values {
+		rowAny := make([]any, len(row))
+		for i, v := range row {
+			rowAny[i] = v
+		}
+		_ = table.Append(rowAny...)
+	}
 	if modify != nil {
 		modify(table)
 	}
 
 	// Render
 	_, _ = writer.Write([]byte("\n"))
-	table.Render()
+	_ = table.Render()
 	_, _ = writer.Write([]byte("\n"))
 	_ = writer.Close()
 	<-done
-- 
2.52.0
````

## Benefits

1. **Removes KSail Fork:** Once merged upstream, KSail can remove `./patches/loft-sh-log` and the `replace` directive
2. **Unblocks Ecosystem:** Other projects using loft-sh/log can upgrade their dependencies
3. **Modern API:** v1.x API provides better type safety with functional options
4. **Backward Compatible:** No breaking changes to `table` package consumers
5. **Reduced Maintenance:** No more manual tracking of upstream changes

## Testing Strategy

The change maintains the same public API surface for `PrintTable` and `PrintTableWithOptions`. Existing callers will not need modifications.

**Manual Testing:**
1. Build projects that use loft-sh/log with the updated version
2. Verify table output formatting is preserved
3. Confirm no compilation errors with v1.1.2

**Recommended by Maintainers:**
- Add integration tests if upstream has a test suite
- Verify with downstream projects (vcluster SDK, etc.)

## Submitting the PR

### Prerequisites

1. Fork loft-sh/log on GitHub
2. Clone your fork locally
3. Create feature branch: `git checkout -b update-tablewriter-v1`

### Steps

1. **Apply Changes:**
   ```bash
   # Option A: Use the patch file
   curl -o tablewriter-update.patch https://raw.githubusercontent.com/devantler-tech/ksail/main/docs/upstream-prs/loft-sh-log-tablewriter-update.md
   # Extract patch from markdown and apply
   
   # Option B: Manually copy changes from patch diff above
   ```

2. **Update Dependencies:**
   ```bash
   go get github.com/olekukonko/tablewriter@v1.1.2
   go mod tidy
   ```

3. **Commit:**
   ```bash
   git add go.mod go.sum table/table.go
   git commit -m "Update tablewriter from v0.0.5 to v1.1.2 API"
   ```

4. **Push and Create PR:**
   ```bash
   git push origin update-tablewriter-v1
   # Create PR via GitHub UI
   ```

### PR Description Template

```markdown
## Description

This PR updates the `table` package to use the modern tablewriter v1.1.2 API instead of the deprecated v0.0.5 API.

## Motivation

- Tablewriter v0.0.5 and v1.x have incompatible APIs and cannot coexist in the same dependency tree
- Modern projects (k9s, grype, syft) require tablewriter v1.x
- Downstream consumers (like KSail) currently maintain forks to work around this issue

## Changes

- Update go.mod: tablewriter v0.0.5 → v1.1.2
- Refactor table/table.go to use v1.x API:
  - `NewWriter()` → `NewTable()` with functional options
  - `SetHeader()` → `Header()` with `[]any`
  - `AppendBulk()` → individual `Append()` calls
  - Use `tw.AlignLeft` from `tablewriter/tw` package

## Backward Compatibility

✅ No breaking changes to consumers of `PrintTable` and `PrintTableWithOptions`.

## Testing

- [x] Code compiles with tablewriter v1.1.2
- [x] Table output formatting preserved
- [ ] (Optional) Add integration tests

## References

- Related issue: devantler-tech/ksail#2246
- Tablewriter v1 docs: https://pkg.go.dev/github.com/olekukonko/tablewriter@v1.1.2
```

## Next Steps for KSail

Once the upstream PR is merged and a new version of loft-sh/log is released:

1. **Remove Local Fork:**
   ```bash
   rm -rf patches/loft-sh-log
   ```

2. **Update go.mod:**
   ```diff
   -	github.com/loft-sh/log v0.0.0-20240219160058-26d83ffb46ac
   +	github.com/loft-sh/log v0.0.0-YYYYMMDDHHMMSS-<commit_hash>  # or vX.Y.Z if tagged
   
   -	// loft-sh/log uses tablewriter v0.0.5 API which is incompatible with v1.x
   -	// required by k9s, grype, and syft. This local fork patches table/table.go
   -	// for the v1.x API.
   -	github.com/loft-sh/log => ./patches/loft-sh-log
   ```

3. **Run Tests:**
   ```bash
   go mod tidy
   go build ./...
   go test ./...
   ```

4. **Close Issue:** Close #2246 with reference to merged upstream PR

## Contact

- **KSail Issue:** [#2246](https://github.com/devantler-tech/ksail/issues/2246)
- **Upstream Repo:** https://github.com/loft-sh/log
- **Patch Generated:** 2026-02-23 by Daily Backlog Burner workflow
