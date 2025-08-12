package inputs

import (
	"testing"

	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
)

func TestSetInputsOrFallback(t *testing.T) {
	t.Run("overrides only non-zero CLI inputs", func(t *testing.T) {
		// Arrange: create cluster with defaults
		cluster := ksailcluster.NewCluster()
		original := *cluster // copy for later comparison

		// Set some CLI inputs (simulate flags)
		Name = "custom-name"
		Distribution = ksailcluster.DistributionK3d
		ReconciliationTool = ksailcluster.ReconciliationToolFlux
		// Leave SourceDirectory empty so fallback should remain default
		SourceDirectory = ""
		// ContainerEngine left zero so fallback remains default

		// Act
		SetInputsOrFallback(cluster)

		// Assert overridden
		if cluster.Metadata.Name != "custom-name" {
			t.Fatalf("expected name overridden to 'custom-name', got %q", cluster.Metadata.Name)
		}
		if cluster.Spec.Distribution != ksailcluster.DistributionK3d {
			t.Fatalf("expected distribution overridden to %q, got %q", ksailcluster.DistributionK3d, cluster.Spec.Distribution)
		}
		if cluster.Spec.ReconciliationTool != ksailcluster.ReconciliationToolFlux {
			t.Fatalf("expected reconciliation tool overridden to %q, got %q", ksailcluster.ReconciliationToolFlux, cluster.Spec.ReconciliationTool)
		}
		// Assert fallbacks preserved
		if cluster.Spec.SourceDirectory != original.Spec.SourceDirectory {
			t.Fatalf("expected source directory fallback %q preserved, got %q", original.Spec.SourceDirectory, cluster.Spec.SourceDirectory)
		}
		if cluster.Spec.ContainerEngine != original.Spec.ContainerEngine {
			t.Fatalf("expected container engine fallback %q preserved, got %q", original.Spec.ContainerEngine, cluster.Spec.ContainerEngine)
		}

		// Cleanup mutated global inputs for other tests
		Name = ""
		Distribution = ""
		ReconciliationTool = ""
	})
}

func TestinputOrFallback(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		if got := inputOrFallback("", "fallback"); got != "fallback" {
			t.Fatalf("expected 'fallback', got '%q'", got)
		}
		if got := inputOrFallback("value", "fallback"); got != "value" {
			t.Fatalf("expected 'value', got '%q'", got)
		}
	})
	t.Run("bool", func(t *testing.T) {
		if got := inputOrFallback(false, true); got != true {
			t.Fatalf("expected 'false', got '%v'", got)
		}
		if got := inputOrFallback(true, false); got != true {
			t.Fatalf("expected 'true', got '%v'", got)
		}
	})
	t.Run("int variants", func(t *testing.T) {
		if got := inputOrFallback(0, 7); got != 7 {
			t.Fatalf("int zero: expected '7', got '%d'", got)
		}
		if got := inputOrFallback(5, 7); got != 5 {
			t.Fatalf("int non-zero: expected '5', got '%d'", got)
		}
	})
	t.Run("float variants", func(t *testing.T) {
		if got := inputOrFallback(0, 1.5); got != 1.5 {
			t.Fatalf("float32 zero: expected '1.5', got '%v'", got)
		}
		if got := inputOrFallback(2.5, 1.5); got != 2.5 {
			t.Fatalf("float32 non-zero: expected '2.5', got '%v'", got)
		}
		if got := inputOrFallback(0, 3.25); got != 3.25 {
			t.Fatalf("float64 zero: expected '3.25', got '%v'", got)
		}
		if got := inputOrFallback(4.75, 3.25); got != 4.75 {
			t.Fatalf("float64 non-zero: expected '4.75', got '%v'", got)
		}
	})
	t.Run("complex variants", func(t *testing.T) {
		if got := inputOrFallback(0+0i, 1+2i); got != 1+2i {
			t.Fatalf("complex64 zero: expected '(1+2i)', got '%v'", got)
		}
		if got := inputOrFallback(3+4i, 1+2i); got != 3+4i {
			t.Fatalf("complex64 non-zero: expected '(3+4i)', got '%v'", got)
		}
		if got := inputOrFallback(0+0i, 5+6i); got != 5+6i {
			t.Fatalf("complex128 zero: expected '(5+6i)', got '%v'", got)
		}
		if got := inputOrFallback(7+8i, 5+6i); got != 7+8i {
			t.Fatalf("complex128 non-zero: expected '(7+8i)', got '%v'", got)
		}
	})
	t.Run("struct", func(t *testing.T) {
		type testStruct struct {
			Field string
		}
		if got := inputOrFallback(testStruct{}, testStruct{Field: "fallback"}); got != (testStruct{Field: "fallback"}) {
			t.Fatalf("expected 'fallback', got '%v'", got)
		}
		if got := inputOrFallback(testStruct{Field: "value"}, testStruct{Field: "fallback"}); got != (testStruct{Field: "value"}) {
			t.Fatalf("expected 'value', got '%v'", got)
		}
	})
	t.Run("enum", func(t *testing.T) {
		type testEnum string
		const (
			EnumZero testEnum = "Value1"
			EnumOne  testEnum = "Value2"
		)
		if got := inputOrFallback(EnumZero, EnumOne); got != EnumZero {
			t.Fatalf("expected '0', got '%v'", got)
		}
		if got := inputOrFallback(EnumOne, EnumZero); got != EnumOne {
			t.Fatalf("expected '1', got '%v'", got)
		}
	})
}
