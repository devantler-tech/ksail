package eksprovisioner

import (
	"fmt"
	"io"
	"sync"
)

// PreviewFeedbackURL is the canonical location for users to file EKS feedback.
const PreviewFeedbackURL = "https://github.com/devantler-tech/ksail/issues/new?labels=eks,preview"

const previewBanner = "⚠ EKS support is a PREVIEW feature. Expect rough edges. " +
	"Please report issues and feedback at " + PreviewFeedbackURL

//nolint:gochecknoglobals // process-wide guard so the banner only prints once.
var previewOnce sync.Once

// EmitPreviewBanner writes the EKS preview notice to writer at most once per process.
// It is intended to be called from CLI entry points that exercise the EKS
// distribution so users know the feature is still being stabilised and where
// to send feedback.
func EmitPreviewBanner(writer io.Writer) {
	if writer == nil {
		return
	}

	previewOnce.Do(func() {
		_, _ = fmt.Fprintln(writer, previewBanner)
	})
}

// ResetPreviewBannerForTest re-arms the sync.Once guard. Used by tests to
// verify the banner is only emitted once per process.
func ResetPreviewBannerForTest() {
	previewOnce = sync.Once{}
}
