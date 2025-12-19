package kubeconform_test

import (
	"testing"

	"github.com/devantler-tech/ksail/pkg/client/kubeconform"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	client := kubeconform.NewClient()

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClientWithSchemaLocation(t *testing.T) {
	t.Parallel()

	customLocation := "/custom/schema/location"
	client := kubeconform.NewClientWithSchemaLocation(customLocation)

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestValidationOptions(t *testing.T) {
	t.Parallel()

	opts := &kubeconform.ValidationOptions{
		SkipKinds:            []string{"Secret", "ConfigMap"},
		Strict:               true,
		IgnoreMissingSchemas: true,
		Verbose:              false,
	}

	if len(opts.SkipKinds) != 2 {
		t.Fatalf("expected 2 skip kinds, got %d", len(opts.SkipKinds))
	}

	if opts.SkipKinds[0] != "Secret" {
		t.Fatalf("expected first skip kind to be Secret, got %s", opts.SkipKinds[0])
	}

	if !opts.Strict {
		t.Fatal("expected Strict to be true")
	}

	if !opts.IgnoreMissingSchemas {
		t.Fatal("expected IgnoreMissingSchemas to be true")
	}

	if opts.Verbose {
		t.Fatal("expected Verbose to be false")
	}
}
