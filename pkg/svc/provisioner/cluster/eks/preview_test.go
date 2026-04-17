package eksprovisioner_test

import (
	"bytes"
	"strings"
	"testing"

	eksprovisioner "github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/eks"
)

func TestEmitPreviewBanner(t *testing.T) {
	t.Parallel()

	eksprovisioner.ResetPreviewBannerForTest()

	var buf bytes.Buffer

	eksprovisioner.EmitPreviewBanner(&buf)

	got := buf.String()
	if !strings.Contains(got, "PREVIEW") {
		t.Fatalf("expected banner to mention PREVIEW, got %q", got)
	}

	if !strings.Contains(got, eksprovisioner.PreviewFeedbackURL) {
		t.Fatalf(
			"expected banner to include feedback URL %q, got %q",
			eksprovisioner.PreviewFeedbackURL,
			got,
		)
	}

	// Second call must be a no-op within a single process.
	var second bytes.Buffer
	eksprovisioner.EmitPreviewBanner(&second)

	if second.Len() != 0 {
		t.Fatalf("expected second emission to be suppressed, got %q", second.String())
	}
}

func TestEmitPreviewBanner_NilWriter(t *testing.T) {
	t.Parallel()

	eksprovisioner.ResetPreviewBannerForTest()

	// Must not panic with a nil writer.
	eksprovisioner.EmitPreviewBanner(nil)
}
