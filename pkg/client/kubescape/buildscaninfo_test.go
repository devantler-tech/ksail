package kubescape_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubescape"
	"github.com/kubescape/kubescape/v3/core/cautils"
	apisv1 "github.com/kubescape/opa-utils/httpserver/apis/v1"
)

const (
	testPath       = "./manifests"
	testFormatJSON = "json"
	testOutputPath = "/tmp/results.json"
	testFwNSA      = "nsa"
	testFwMitre    = "mitre"
)

// TestBuildScanInfoDefaults asserts the constant base configuration applied for
// an empty ScanOptions: the input path is scanned locally as a repo, and no
// optional fields are populated.
func TestBuildScanInfoDefaults(t *testing.T) {
	t.Parallel()

	info := kubescape.BuildScanInfo(testPath, &kubescape.ScanOptions{})

	if len(info.InputPatterns) != 1 || info.InputPatterns[0] != testPath {
		t.Fatalf("expected InputPatterns [%q], got %v", testPath, info.InputPatterns)
	}

	checks := []struct {
		name string
		ok   bool
	}{
		{"Local true", info.Local},
		{"ScanType repo", info.ScanType == cautils.ScanTypeRepo},
		{"VerboseMode false", !info.VerboseMode},
		{"empty Format", info.Format == ""},
		{"empty Output", info.Output == ""},
		{"zero ComplianceThreshold", info.ComplianceThreshold == 0},
		{"FrameworkScan false", !info.FrameworkScan},
		{"no policy identifiers", len(info.PolicyIdentifier) == 0},
	}

	for _, check := range checks {
		if !check.ok {
			t.Errorf("default ScanInfo: %s assertion failed", check.name)
		}
	}
}

// TestBuildScanInfoVerbose asserts Verbose maps to VerboseMode.
func TestBuildScanInfoVerbose(t *testing.T) {
	t.Parallel()

	info := kubescape.BuildScanInfo(testPath, &kubescape.ScanOptions{Verbose: true})

	if !info.VerboseMode {
		t.Error("expected VerboseMode to be true when Verbose is set")
	}
}

// TestBuildScanInfoFormatAndOutput asserts non-empty Format/Output are forwarded.
func TestBuildScanInfoFormatAndOutput(t *testing.T) {
	t.Parallel()

	info := kubescape.BuildScanInfo(testPath, &kubescape.ScanOptions{
		Format: testFormatJSON,
		Output: testOutputPath,
	})

	if info.Format != testFormatJSON {
		t.Errorf("expected Format %q, got %q", testFormatJSON, info.Format)
	}

	if info.Output != testOutputPath {
		t.Errorf("expected Output %q, got %q", testOutputPath, info.Output)
	}
}

// TestBuildScanInfoComplianceThreshold asserts a positive threshold is forwarded
// and that a zero threshold leaves the field untouched (guarded by `> 0`).
func TestBuildScanInfoComplianceThreshold(t *testing.T) {
	t.Parallel()

	info := kubescape.BuildScanInfo(testPath, &kubescape.ScanOptions{ComplianceThreshold: 80})
	if info.ComplianceThreshold != 80 {
		t.Errorf("expected ComplianceThreshold 80, got %v", info.ComplianceThreshold)
	}

	zero := kubescape.BuildScanInfo(testPath, &kubescape.ScanOptions{ComplianceThreshold: 0})
	if zero.ComplianceThreshold != 0 {
		t.Errorf("expected ComplianceThreshold 0, got %v", zero.ComplianceThreshold)
	}
}

// TestBuildScanInfoFrameworks asserts frameworks enable a framework scan and are
// registered as Framework-kind policy identifiers preserving order.
func TestBuildScanInfoFrameworks(t *testing.T) {
	t.Parallel()

	frameworks := []string{testFwNSA, testFwMitre, "cis"}
	info := kubescape.BuildScanInfo(testPath, &kubescape.ScanOptions{Frameworks: frameworks})

	if !info.FrameworkScan {
		t.Error("expected FrameworkScan to be true when frameworks are provided")
	}

	if len(info.PolicyIdentifier) != len(frameworks) {
		t.Fatalf(
			"expected %d policy identifiers, got %d",
			len(frameworks),
			len(info.PolicyIdentifier),
		)
	}

	for idx, want := range frameworks {
		got := info.PolicyIdentifier[idx]
		if got.Identifier != want {
			t.Errorf("policy[%d]: expected identifier %q, got %q", idx, want, got.Identifier)
		}

		if got.Kind != apisv1.KindFramework {
			t.Errorf("policy[%d]: expected kind %q, got %q", idx, apisv1.KindFramework, got.Kind)
		}
	}
}

// TestBuildScanInfoExceptions asserts a non-empty Exceptions path is forwarded to
// UseExceptions (the field Kubescape's --exceptions flag sets) and that an empty
// Exceptions leaves UseExceptions untouched.
func TestBuildScanInfoExceptions(t *testing.T) {
	t.Parallel()

	const exceptionsPath = "/tmp/exceptions.json"

	info := kubescape.BuildScanInfo(testPath, &kubescape.ScanOptions{Exceptions: exceptionsPath})
	if info.UseExceptions != exceptionsPath {
		t.Errorf("expected UseExceptions %q, got %q", exceptionsPath, info.UseExceptions)
	}

	empty := kubescape.BuildScanInfo(testPath, &kubescape.ScanOptions{})
	if empty.UseExceptions != "" {
		t.Errorf("expected empty UseExceptions, got %q", empty.UseExceptions)
	}
}

// TestScanDirectoryNilOptionsCancelledContext asserts that nil options are
// tolerated (defaulted) and that a cancelled context short-circuits the scan
// with a wrapped context error before any kubescape runner is constructed.
func TestScanDirectoryNilOptionsCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := kubescape.NewClient()

	result, err := client.ScanDirectory(ctx, testPath, nil)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	if result != nil {
		t.Errorf("expected no result from a cancelled scan, got %+v", result)
	}
}

// TestScanDirectoryCancelledContext asserts the context guard fires with
// non-nil options as well.
func TestScanDirectoryCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := kubescape.NewClient()

	result, err := client.ScanDirectory(
		ctx,
		testPath,
		&kubescape.ScanOptions{Frameworks: []string{testFwNSA}},
	)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	if result != nil {
		t.Errorf("expected no result from a cancelled scan, got %+v", result)
	}
}

// TestCheckComplianceThreshold pins the compliance gate: a score below a
// positive threshold fails with both values in the message, and a score at or
// above it, or a zero (disabled) threshold, passes.
func TestCheckComplianceThreshold(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		score     float32
		threshold float32
		wantErr   string
	}{
		{
			name:      "below threshold fails",
			score:     94.99,
			threshold: 100,
			wantErr:   "compliance score 94.99% is below threshold 100.00%",
		},
		{name: "at threshold passes", score: 85, threshold: 85},
		{name: "above threshold passes", score: 98.87, threshold: 85},
		{name: "zero threshold disables the gate", score: 0, threshold: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := kubescape.CheckComplianceThreshold(test.score, test.threshold)

			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("expected the gate to pass, got %v", err)
				}

				return
			}

			if !errors.Is(err, kubescape.ErrScanFailed) {
				t.Fatalf("expected ErrScanFailed, got %v", err)
			}

			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("expected error to contain %q, got %q", test.wantErr, err.Error())
			}
		})
	}
}
