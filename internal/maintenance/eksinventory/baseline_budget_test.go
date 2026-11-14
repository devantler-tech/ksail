//go:build unix

package eksinventory_test

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/maintenance/eksinventory"
)

func TestBaselineEvidenceLimitDiscardsWholeFamily(t *testing.T) {
	t.Parallel()

	page := 0
	query := baselineQuery(t, func(context.Context, ...string) ([]byte, error) {
		data := largeBaselinePage(t, page)
		page++

		return data, nil
	})
	path := filepath.Join(privateBaselineDir(t), "baseline.json")

	err := eksinventory.CaptureBaseline(
		context.Background(),
		baselineBinding(),
		path,
		io.Discard,
		query,
	)
	if err == nil {
		t.Fatal("oversized capture accepted")
	}

	family := volumeObservation(t, readBaseline(t, path))
	if family.Complete || family.Identities != nil || family.ReportCount != nil ||
		family.Reason != "evidence-limit" {
		t.Fatalf("oversized family retained usable evidence: %#v", family)
	}
}

func largeBaselinePage(t *testing.T, page int) []byte {
	t.Helper()

	const (
		recordsPerPage = 1000
		identitySize   = 3000
		pageCount      = 6
	)

	rows := make([]map[string]string, recordsPerPage)
	for index := range rows {
		rows[index] = map[string]string{
			"VolumeId": strings.Repeat("x", identitySize) + strconv.Itoa(page*recordsPerPage+index),
			"State":    "available",
		}
	}

	response := map[string]any{"Volumes": rows}
	if page+1 < pageCount {
		response["NextToken"] = strconv.Itoa(page + 1)
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}

	return data
}
