package kubescape_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubescape"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	client := kubescape.NewClient()

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestScanOptions(t *testing.T) {
	t.Parallel()

	opts := &kubescape.ScanOptions{
		Frameworks:          []string{"nsa", "mitre"},
		Format:              "json",
		Output:              "/tmp/results.json",
		ComplianceThreshold: 80,
		Verbose:             true,
	}

	if len(opts.Frameworks) != 2 {
		t.Fatalf("expected 2 frameworks, got %d", len(opts.Frameworks))
	}

	if opts.Frameworks[0] != "nsa" {
		t.Fatalf("expected first framework to be %q, got %q", "nsa", opts.Frameworks[0])
	}

	if opts.Frameworks[1] != "mitre" {
		t.Fatalf("expected second framework to be %q, got %q", "mitre", opts.Frameworks[1])
	}

	if opts.Format != "json" {
		t.Fatalf("expected Format to be %q, got %q", "json", opts.Format)
	}

	if opts.Output != "/tmp/results.json" {
		t.Fatalf("expected Output to be %q, got %q", "/tmp/results.json", opts.Output)
	}

	if opts.ComplianceThreshold != 80 {
		t.Fatalf("expected ComplianceThreshold to be 80, got %v", opts.ComplianceThreshold)
	}

	if !opts.Verbose {
		t.Fatal("expected Verbose to be true")
	}
}

func TestScanOptionsDefaults(t *testing.T) {
	t.Parallel()

	opts := &kubescape.ScanOptions{}

	if opts.Frameworks != nil {
		t.Fatalf("expected Frameworks to be nil, got %v", opts.Frameworks)
	}

	if opts.Format != "" {
		t.Fatalf("expected Format to be empty, got %q", opts.Format)
	}

	if opts.Output != "" {
		t.Fatalf("expected Output to be empty, got %q", opts.Output)
	}

	if opts.ComplianceThreshold != 0 {
		t.Fatalf("expected ComplianceThreshold to be 0, got %v", opts.ComplianceThreshold)
	}

	if opts.Verbose {
		t.Fatal("expected Verbose to be false")
	}
}
