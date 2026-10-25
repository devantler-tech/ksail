// Copyright (c) KSail contributors. All rights reserved.
// Licensed under the PolyForm Shield License 1.0.0. See LICENSE in the project root.

package docs_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/docs"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const supportMatrixPagePath = "src/content/docs/support-matrix.mdx"

// TestSupportMatrixTableInSync fails when the distribution metadata changed and the
// generated table in the support matrix page was not regenerated.
func TestSupportMatrixTableInSync(t *testing.T) {
	t.Parallel()

	page := readOrSkip(t, supportMatrixPagePath)

	table, err := docs.RenderDistributionProviderTable()
	require.NoError(t, err)

	regenerated, err := docs.ReplaceDistributionProviderTable(page, table)
	require.NoError(t, err)
	assert.Equal(
		t,
		regenerated,
		page,
		supportMatrixPagePath+" has a stale distribution × provider table; run go generate ./docs/...",
	)
}

func TestReplaceDistributionProviderTable(t *testing.T) {
	t.Parallel()

	begin := docs.SupportMatrixBeginMarker
	end := docs.SupportMatrixEndMarker

	got, err := docs.ReplaceDistributionProviderTable(
		"# Page\n\n"+begin+"\n| stale |\n"+end+"\n\nNotes stay.\n",
		"| fresh |\n",
	)
	require.NoError(t, err)
	assert.Equal(t, "# Page\n\n"+begin+"\n\n| fresh |\n\n"+end+"\n\nNotes stay.\n", got)

	for name, page := range map[string]string{
		"no markers":      "# Page\n",
		"no end marker":   begin + "\n",
		"end before":      end + "\n" + begin + "\n",
		"two tables":      begin + end + begin + end,
		"no begin marker": end + "\n",
	} {
		_, err := docs.ReplaceDistributionProviderTable(page, "| fresh |\n")
		require.ErrorIs(t, err, docs.ErrSupportMatrixMarkers, name)
	}
}

// TestRenderDistributionProviderTable_KnownCombinations anchors the generated table to
// combinations documented on the provider and distribution pages.
func TestRenderDistributionProviderTable_KnownCombinations(t *testing.T) {
	t.Parallel()

	table, err := docs.RenderDistributionProviderTable()
	require.NoError(t, err)

	rows := supportMatrixRows(t, table)

	tests := []struct {
		distribution string
		provider     string
		want         string
	}{
		{"Talos", "Omni", "✅"},
		{"Talos", "Hetzner", "✅"},
		{"Vanilla (Kind)", "Hetzner", "✅"},
		{"K3s (K3d)", "Kubernetes", "✅"},
		{"VCluster (Vind)", "Hetzner", "❌"},
		{"KWOK (kwokctl)", "Omni", "❌"},
		{"EKS", "AWS", "✅"},
		{"EKS", "Docker", "❌"},
		{"GKE", "GCP", "✅"},
		{"AKS", "Azure", "✅"},
		{"AKS", "AWS", "❌"},
	}

	for _, test := range tests {
		row, found := rows[test.distribution]
		require.True(t, found, "missing row %q", test.distribution)
		assert.Equal(t, test.want, row[test.provider],
			"%s on %s", test.distribution, test.provider)
	}

	assert.Len(t, rows, len(v1alpha1.ValidDistributions()),
		"every distribution needs exactly one row")
}

func TestSupportMatrixCell(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "❌", docs.SupportMatrixCell(false, false))
	assert.Equal(t, "❌", docs.SupportMatrixCell(false, true))
	assert.Equal(t, "✅", docs.SupportMatrixCell(true, false))
	assert.Equal(t, "✅🧪", docs.SupportMatrixCell(true, true))
}

// supportMatrixRows parses the rendered table into row label → provider column → cell.
func supportMatrixRows(t *testing.T, table string) map[string]map[string]string {
	t.Helper()

	var header []string

	rows := make(map[string]map[string]string)

	for line := range strings.SplitSeq(table, "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| ---") {
			continue
		}

		cells := strings.Split(strings.Trim(line, "| "), " | ")
		if header == nil {
			header = cells

			continue
		}

		require.Len(t, cells, len(header), "row %q", line)

		row := make(map[string]string, len(header)-1)
		for index, provider := range header[1:] {
			row[provider] = cells[index+1]
		}

		rows[cells[0]] = row
	}

	require.NotNil(t, header, "the rendered text has no table")

	return rows
}
