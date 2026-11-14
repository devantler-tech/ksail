//go:build unix

package eksinventory_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/maintenance/eksinventory"
)

type baselineSnapshot struct {
	Context struct {
		RunAttempt string `json:"runAttempt"`
	} `json:"context"`
	Coverage     map[string]string `json:"coverage"`
	Observations []baselineRow     `json:"observations"`
}

type baselineRow struct {
	Service     string   `json:"service"`
	Operation   string   `json:"operation"`
	Complete    bool     `json:"complete"`
	Identities  []string `json:"identities"`
	ReportCount *int     `json:"reportCount"`
	Reason      string   `json:"reason"`
}

func baselineBinding() eksinventory.BaselineContext {
	return eksinventory.BaselineContext{
		Region: "eu-central-1", ClusterName: "regional-diagnostic",
		RunID: "123", RunAttempt: "2", SourceSHA: strings.Repeat("a", 40),
	}
}

func privateBaselineDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private")

	err := os.Mkdir(dir, 0o700)
	if err != nil {
		t.Fatal(err)
	}

	return dir
}

func baselineQuery(t *testing.T, change eksinventory.Query) eksinventory.Query {
	t.Helper()
	pages := emptyPages(t)

	return func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "ec2" && args[1] == "describe-volumes" && change != nil {
			return change(ctx, args...)
		}

		return pages[args[0]+" "+args[1]], nil
	}
}

func readBaseline(t *testing.T, path string) baselineSnapshot {
	t.Helper()
	data := readPrivateTestFile(t, path)

	var result baselineSnapshot

	err := json.Unmarshal(data, &result)
	if err != nil {
		t.Fatal(err)
	}

	return result
}

func readPrivateTestFile(t *testing.T, path string) []byte {
	t.Helper()

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = root.Close() })

	data, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func volumeObservation(t *testing.T, manifest baselineSnapshot) baselineRow {
	t.Helper()

	for _, row := range manifest.Observations {
		if row.Service == "ec2" && row.Operation == "describe-volumes" {
			return row
		}
	}

	t.Fatal("missing EBS observation")

	return baselineRow{}
}

func TestBaselinePreservesLaterPageIdentitiesWithoutPublishingThem(t *testing.T) {
	t.Parallel()
	query := baselineQuery(t, func(_ context.Context, args ...string) ([]byte, error) {
		if strings.Contains(args[8], "NextToken") {
			return []byte(`{"Volumes":[{"VolumeId":"private-a","State":"available"}]}`), nil
		}

		return []byte(
			`{"Volumes":[{"VolumeId":"private-z","State":"in-use"}],"NextToken":"page-2"}`,
		), nil
	})
	path := filepath.Join(privateBaselineDir(t), "baseline.json")

	var output bytes.Buffer

	err := eksinventory.CaptureBaseline(
		context.Background(),
		baselineBinding(),
		path,
		&output,
		query,
	)
	if err != nil {
		t.Fatal(err)
	}

	manifest := readBaseline(t, path)

	row := volumeObservation(t, manifest)
	if !row.Complete ||
		!reflect.DeepEqual(row.Identities, []string{"private-a", "private-z"}) {
		t.Fatalf("incomplete or unsorted identity snapshot: %#v", row)
	}

	if manifest.Context.RunAttempt != "2" || len(manifest.Observations) != 14 {
		t.Fatal("lost context or resource families")
	}

	assertUnknownCoverage(t, manifest)

	if strings.Contains(output.String(), "private-") ||
		!strings.Contains(output.String(), "teardown=unverified") {
		t.Fatalf("unsafe baseline summary: %s", output.String())
	}
}

func TestBaselineDiscardsEveryPartialFamily(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name, response string
		err            error
	}{
		{"denied", `{"Volumes":[]}`, fs.ErrPermission},
		{"deadline", `{"Volumes":[]}`, context.DeadlineExceeded},
		{"trailing-json", `{"Volumes":[]} {}`, nil},
		{"missing-collection", `{}`, nil},
		{"repeated-token", `{"Volumes":[],"NextToken":"page-2"}`, nil},
		{"duplicate-identity", `{"Volumes":[{"VolumeId":"private-first","State":"available"}]}`, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			query := baselineQuery(t, func(_ context.Context, args ...string) ([]byte, error) {
				if strings.Contains(args[8], "NextToken") {
					return []byte(test.response), test.err
				}

				return []byte(
					`{"Volumes":[{"VolumeId":"private-first","State":"available"}],"NextToken":"page-2"}`,
				), nil
			})
			path := filepath.Join(privateBaselineDir(t), "baseline.json")

			var output bytes.Buffer

			err := eksinventory.CaptureBaseline(
				context.Background(),
				baselineBinding(),
				path,
				&output,
				query,
			)
			if !errors.Is(err, eksinventory.ErrInventory) {
				t.Fatalf("incomplete capture accepted: %v", err)
			}

			row := volumeObservation(t, readBaseline(t, path))
			if row.Complete || row.Identities != nil || row.ReportCount != nil {
				t.Fatalf("partial data retained as usable evidence: %#v", row)
			}

			if strings.Contains(output.String(), "private-") ||
				!strings.Contains(output.String(), "unknown") {
				t.Fatalf("unsafe incomplete summary: %s", output.String())
			}
		})
	}
}

func TestBaselineRefusesExistingFileAndSymlinkBeforeReadingAWS(t *testing.T) {
	t.Parallel()

	for _, symlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "symlink"}[symlink], func(t *testing.T) {
			t.Parallel()
			dir := privateBaselineDir(t)

			original := filepath.Join(dir, "original")

			err := os.WriteFile(original, []byte("preserve"), 0o600)
			if err != nil {
				t.Fatal(err)
			}

			destination := original
			if symlink {
				destination = filepath.Join(dir, "baseline.json")

				err := os.Symlink(original, destination)
				if err != nil {
					t.Fatal(err)
				}
			}

			query := func(context.Context, ...string) ([]byte, error) {
				t.Fatal("existing destination caused AWS reads")

				return nil, nil
			}

			err = eksinventory.CaptureBaseline(
				context.Background(),
				baselineBinding(),
				destination,
				io.Discard,
				query,
			)
			if err == nil {
				t.Fatal("existing destination overwritten")
			}

			if data := readPrivateTestFile(t, original); string(data) != "preserve" {
				t.Fatal("original evidence changed")
			}
		})
	}
}

func TestBaselineRejectsInvalidBindingsBeforeReadingAWS(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"Region", "ClusterName", "RunID", "RunAttempt", "SourceSHA"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			binding := baselineBinding()
			reflect.ValueOf(&binding).Elem().FieldByName(field).SetString("invalid;value")

			path := filepath.Join(privateBaselineDir(t), "baseline.json")

			query := func(context.Context, ...string) ([]byte, error) {
				t.Fatal("invalid context caused AWS reads")

				return nil, nil
			}

			err := eksinventory.CaptureBaseline(
				context.Background(),
				binding,
				path,
				io.Discard,
				query,
			)
			if err == nil {
				t.Fatal("invalid binding accepted")
			}

			_, statErr := os.Stat(path)
			if !os.IsNotExist(statErr) {
				t.Fatal("invalid context published evidence")
			}
		})
	}
}

func TestBaselineCannotConflateContextFieldsWithEqualValues(t *testing.T) {
	t.Parallel()

	binding := baselineBinding()
	binding.Region = "invalid"
	binding.ClusterName = "invalid"
	path := filepath.Join(privateBaselineDir(t), "baseline.json")

	err := eksinventory.CaptureBaseline(
		context.Background(),
		binding,
		path,
		io.Discard,
		baselineQuery(t, nil),
	)
	if err == nil {
		t.Fatal("invalid region accepted because another field has the same value")
	}

	_, statErr := os.Stat(path)
	if !os.IsNotExist(statErr) {
		t.Fatal("invalid binding published evidence")
	}
}

func TestBaselinePublicationRefusesACollisionCreatedDuringReads(t *testing.T) {
	t.Parallel()
	dir := privateBaselineDir(t)
	path := filepath.Join(dir, "baseline.json")

	query := baselineQuery(t, func(context.Context, ...string) ([]byte, error) {
		err := os.WriteFile(path, []byte("preserve-concurrent-evidence"), 0o600)
		if err != nil {
			t.Fatal(err)
		}

		return []byte(`{"Volumes":[]}`), nil
	})

	err := eksinventory.CaptureBaseline(
		context.Background(),
		baselineBinding(),
		path,
		io.Discard,
		query,
	)
	if err == nil {
		t.Fatal("publication overwrote a concurrent destination")
	}

	data := readPrivateTestFile(t, path)
	if string(data) != "preserve-concurrent-evidence" {
		t.Fatal("concurrent evidence changed")
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("private temporary file was not removed")
	}
}

func assertUnknownCoverage(t *testing.T, manifest baselineSnapshot) {
	t.Helper()

	for _, key := range []string{"ownership", "instantFleets", "accountBinding"} {
		if manifest.Coverage[key] != "unknown" {
			t.Fatalf("unproven coverage marked known: %s", key)
		}
	}
}

func TestBaselineRetainsIdentitiesOmittedFromDiagnosticCounts(t *testing.T) {
	t.Parallel()
	empty := baselineQuery(t, nil)
	query := func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "service-quotas" {
			return []byte(
				`{"Quotas":[{"QuotaCode":"private-unrelated","QuotaName":"Unrelated quota","Value":1}]}`,
			), nil
		}

		return empty(ctx, args...)
	}

	path := filepath.Join(privateBaselineDir(t), "baseline.json")

	err := eksinventory.CaptureBaseline(
		context.Background(),
		baselineBinding(),
		path,
		io.Discard,
		query,
	)
	if err != nil {
		t.Fatal(err)
	}

	manifest := readBaseline(t, path)
	for _, row := range manifest.Observations {
		if row.Service == "service-quotas" {
			if row.ReportCount == nil || *row.ReportCount != 0 ||
				!reflect.DeepEqual(row.Identities, []string{"private-unrelated"}) {
				t.Fatalf("diagnostic filter erased an observed identity: %#v", row)
			}

			return
		}
	}

	t.Fatal("missing quota observation")
}

func TestBaselineCancellationProducesOnlyUnknownEvidence(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	query := func(context.Context, ...string) ([]byte, error) {
		t.Fatal("cancelled capture read AWS")

		return nil, nil
	}

	path := filepath.Join(privateBaselineDir(t), "baseline.json")

	err := eksinventory.CaptureBaseline(
		ctx,
		baselineBinding(),
		path,
		io.Discard,
		query,
	)
	if err == nil {
		t.Fatal("cancelled capture succeeded")
	}

	for _, row := range readBaseline(t, path).Observations {
		if row.Complete || row.Identities != nil || row.Reason != "deadline" {
			t.Fatalf("cancelled capture produced usable evidence: %#v", row)
		}
	}
}

func TestBaselineRejectsAnUnprotectedParentBeforeReadingAWS(t *testing.T) {
	t.Parallel()

	dir := privateBaselineDir(t)
	//nolint:gosec // Deliberately unprotected directory tests the rejection path.
	err := os.Chmod(
		dir,
		0o777,
	)
	if err != nil {
		t.Fatal(err)
	}

	query := func(context.Context, ...string) ([]byte, error) {
		t.Fatal("unprotected parent caused AWS reads")

		return nil, nil
	}

	err = eksinventory.CaptureBaseline(
		context.Background(),
		baselineBinding(),
		filepath.Join(dir, "baseline.json"),
		io.Discard,
		query,
	)
	if err == nil {
		t.Fatal("unprotected destination parent accepted")
	}
}

type baselineBrokenOutput struct{}

func (baselineBrokenOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestBaselineOutputFailurePreservesPublishedEvidence(t *testing.T) {
	t.Parallel()
	path := filepath.Join(privateBaselineDir(t), "baseline.json")

	err := eksinventory.CaptureBaseline(context.Background(), baselineBinding(), path,
		baselineBrokenOutput{}, baselineQuery(t, nil))
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("output failure ignored: %v", err)
	}

	for _, family := range readBaseline(t, path).Observations {
		if !family.Complete {
			t.Fatal("output failure corrupted completed capture")
		}
	}

	err = eksinventory.CaptureBaseline(context.Background(), baselineBinding(), path,
		io.Discard, baselineQuery(t, nil))
	if err == nil {
		t.Fatal("output failure allowed evidence overwrite")
	}
}
