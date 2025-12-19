package kustomize_test

import (
	"testing"

	"github.com/devantler-tech/ksail/pkg/client/kustomize"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	client := kustomize.NewClient()
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}
