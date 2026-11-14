package gonotices_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
	"github.com/stretchr/testify/require"
)

func TestNoticeTextGroupsProvenanceWithoutLosingMaterialOrPackages(t *testing.T) {
	t.Parallel()
	input, directory := noticeInputFixture(t)
	writeNoticeFixture(
		t,
		directory,
		"nested/nested.go",
		"// Copyright 2025 Nested Authors\npackage nested\n",
	)

	nested := input.Packages[0]
	nested.ImportPath += "/nested"
	nested.Directory = filepath.Join(directory, "nested")
	nested.GoFiles = []string{"nested.go"}
	input.Packages = append(input.Packages, nested)
	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.Equal(
		t,
		1,
		strings.Count(string(payload.Text), "Component: example.com/component@v1.2.3"),
	)
	require.Contains(t, string(payload.Text), "example.com/component\n")
	require.Contains(t, string(payload.Text), "example.com/component/nested\n")
	require.Contains(t, string(payload.Text), "Copyright 2025 Selected Authors")
	require.Contains(t, string(payload.Text), "Copyright 2025 Nested Authors")
	require.Contains(t, string(payload.Text), "manifest.json")
}

func TestNoticeTextDoesNotMislabelUpstreamChecksumAsLocalSourceIdentity(t *testing.T) {
	t.Parallel()
	input, _ := noticeInputFixture(t)
	input.Packages[0].Module.Sum = "h1:unmodified-upstream-content-is-not-selected"
	payload, err := gonotices.Build(input)
	require.NoError(t, err)
	require.NotContains(
		t,
		string(payload.Text),
		"Selected Go source checksum: h1:unmodified-upstream",
	)
	require.Contains(t, string(payload.Text), "Repository source content identity: h1:")
}
