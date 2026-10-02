package ciharness_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// A release-only edit must run the dependency audit before merge, even though the shared Go
// validator skips tests when no Go source or module file changed.
func TestReleaseGraphAuditRunsForReleaseOnlyChanges(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	job, found := workflow.Jobs["release-graph-audit"]
	require.True(t, found, "release-only changes need a dependency-audit job")
	assert.Contains(t, job.Needs, "changes")
	assert.Equal(t, "needs.changes.outputs.release-graph-audit == 'true'", job.If)

	filter := findHarnessStep(t, workflow.Jobs["changes"].Steps, "🔍 Filter paths")
	var filters map[string][]string
	require.NoError(t, yaml.Unmarshal([]byte(stringValue(filter.With["filters"])), &filters))
	patterns := filters["release-graph-audit"]
	require.NotEmpty(t, patterns)

	inputs := auditedReleaseInputPaths(t)
	inputs = append(inputs, ".github/workflows/ci.yaml", "go.mod", "go.sum",
		".govulncheck-allow.txt", "pkg/client/kubescape/cilium_linkage_test.go",
		"desktop/main.go", "internal/ciharness/release_graph_workflow_test.go")
	for _, input := range inputs {
		assert.Truef(t, contractFilterMatches(t, patterns, input),
			"changing only %s must run the shipped dependency audit", input)
	}
	assert.False(t, contractFilterMatches(t, patterns, "README.md"),
		"the release audit should not be selected by unrelated prose")

	aggregate := workflow.Jobs["require-checks-in-pr"]
	assert.Contains(t, aggregate.Needs, "release-graph-audit")
	assert.Contains(t, aggregate.Steps[0].With["job-results"], "${{ needs.release-graph-audit.result }}")
}

// Discover the inputs the release audit pins so adding another hashed file cannot silently leave
// its release-only edits outside the pre-merge gate. The audit separately rejects missing digests.
func auditedReleaseInputPaths(t *testing.T) []string {
	t.Helper()

	contents := readRepoFile(t, "pkg/client/kubescape/release_graph_test.go")
	source, err := parser.ParseFile(token.NewFileSet(), "release_graph_test.go", contents, 0)
	require.NoError(t, err)

	var paths []string
	for _, declaration := range source.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "auditedReleaseConfigDigests" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			entry, isEntry := node.(*ast.KeyValueExpr)
			if !isEntry {
				return true
			}
			key, isString := entry.Key.(*ast.BasicLit)
			require.True(t, isString)
			path, unquoteErr := strconv.Unquote(key.Value)
			require.NoError(t, unquoteErr)
			path, _, _ = strings.Cut(path, "#")
			paths = append(paths, path)
			return false
		})
	}
	require.NotEmpty(t, paths, "release input discovery must examine the audited configuration")

	return paths
}

func TestReleaseGraphAuditPropagatesFailure(t *testing.T) {
	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	job, found := workflow.Jobs["release-graph-audit"]
	require.True(t, found, "release-only changes need a dependency-audit job")
	step := findHarnessStep(t, job.Steps, "🔐 Audit Shipped Dependency Graphs")
	require.NotEmpty(t, step.Run)
	assert.Empty(t, step.If, "a selected audit must not skip its test step")
	assert.False(t, step.ContinueOnError)

	directory := t.TempDir()
	arguments := filepath.Join(directory, "arguments")
	writeExecutableStub(t, filepath.Join(directory, "go"),
		"#!/bin/sh\nprintf '%s\\n' \"$@\" >\"$AUDIT_ARGUMENTS\"\nexit 1\n")
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AUDIT_ARGUMENTS", arguments)
	command := exec.CommandContext(t.Context(), "bash", "-e", "-c", step.Run)
	output, err := command.CombinedOutput()
	require.Errorf(t, err, "a failed dependency audit must fail the workflow step: %s", output)
	actual, readErr := os.ReadFile(arguments) //nolint:gosec // test-owned temporary file.
	require.NoError(t, readErr)
	assert.Contains(t, strings.Split(string(actual), "\n"), "./pkg/client/kubescape/...")
	assert.Contains(t, strings.Split(string(actual), "\n"), "-count=1",
		"release configuration changes must not replay a cached successful test")
}
