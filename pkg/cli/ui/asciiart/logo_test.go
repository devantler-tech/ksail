package asciiart_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/asciiart"
)

// logoLineCount is the expected number of lines in the block-letter logo.
// The chat TUI's defaultLogoHeight relies on this layout.
const logoLineCount = 6

func TestLogo(t *testing.T) {
	t.Parallel()

	logo := asciiart.Logo()

	if logo == "" {
		t.Fatal("expected non-empty logo")
	}

	lines := strings.Split(logo, "\n")
	if len(lines) != logoLineCount {
		t.Errorf("expected %d logo lines, got %d", logoLineCount, len(lines))
	}

	if !strings.Contains(logo, "█") {
		t.Error("expected logo to contain block-letter characters")
	}
}
