package chat_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
)

// TestQuotaDisplay_NoSnapshots tests that no quota text is shown without snapshots.
func TestQuotaDisplay_NoSnapshots(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	result := chat.ExportBuildQuotaStatusText(model)

	if result != "" {
		t.Errorf("expected empty quota text without snapshots, got %q", result)
	}
}

// TestQuotaDisplay_CountedEntitlement tests quota display with counted premium requests.
func TestQuotaDisplay_CountedEntitlement(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetLastQuotaSnapshots(model, map[string]chat.QuotaSnapshotForTest{
		"premium": chat.ExportNewQuotaSnapshot(300, 150, 50, false, "Jan 2"),
	})

	result := chat.ExportBuildQuotaStatusText(model)

	if !strings.Contains(result, "150/300 reqs") {
		t.Errorf("expected '150/300 reqs' in quota text, got %q", result)
	}

	if !strings.Contains(result, "50%") {
		t.Errorf("expected '50%%' in quota text, got %q", result)
	}

	if !strings.Contains(result, "resets Jan 2") {
		t.Errorf("expected 'resets Jan 2' in quota text, got %q", result)
	}
}

// TestQuotaDisplay_UnlimitedEntitlement tests quota display for unlimited entitlement.
func TestQuotaDisplay_UnlimitedEntitlement(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetLastQuotaSnapshots(model, map[string]chat.QuotaSnapshotForTest{
		"premium": chat.ExportNewQuotaSnapshot(0, 0, 0, true, ""),
	})

	result := chat.ExportBuildQuotaStatusText(model)

	if !strings.Contains(result, "∞ reqs") {
		t.Errorf("expected '∞ reqs' for unlimited entitlement, got %q", result)
	}
}

// TestQuotaDisplay_NonPremiumCategory tests that non-premium categories are ignored.
func TestQuotaDisplay_NonPremiumCategory(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetLastQuotaSnapshots(model, map[string]chat.QuotaSnapshotForTest{
		"chat": chat.ExportNewQuotaSnapshot(1000, 500, 50, false, "Feb 1"),
	})

	result := chat.ExportBuildQuotaStatusText(model)

	if result != "" {
		t.Errorf("expected empty quota text for non-premium category, got %q", result)
	}
}

// TestQuotaDisplay_NoResetDate tests quota display without a reset date.
func TestQuotaDisplay_NoResetDate(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetLastQuotaSnapshots(model, map[string]chat.QuotaSnapshotForTest{
		"premium": chat.ExportNewQuotaSnapshot(300, 100, 67, false, ""),
	})

	result := chat.ExportBuildQuotaStatusText(model)

	if !strings.Contains(result, "100/300 reqs") {
		t.Errorf("expected '100/300 reqs' in quota text, got %q", result)
	}

	if strings.Contains(result, "resets") {
		t.Error("expected no 'resets' text when reset date is empty")
	}
}

// TestQuotaDisplay_FullUsage tests quota display when all requests are used.
func TestQuotaDisplay_FullUsage(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetLastQuotaSnapshots(model, map[string]chat.QuotaSnapshotForTest{
		"premium": chat.ExportNewQuotaSnapshot(300, 300, 0, false, "Mar 1"),
	})

	result := chat.ExportBuildQuotaStatusText(model)

	if !strings.Contains(result, "300/300 reqs") {
		t.Errorf("expected '300/300 reqs' for full usage, got %q", result)
	}

	if !strings.Contains(result, "0%") {
		t.Errorf("expected '0%%' remaining in quota text, got %q", result)
	}
}

// TestQuotaDisplay_InFooter tests that quota text appears in the footer when width allows.
func TestQuotaDisplay_InFooter(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetLastQuotaSnapshots(model, map[string]chat.QuotaSnapshotForTest{
		"premium": chat.ExportNewQuotaSnapshot(300, 50, 83, false, "Jan 15"),
	})

	// Widen the terminal so there is enough room for help text + quota
	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 200, Height: 40})

	output := updatedModel.View()

	if !strings.Contains(output, "50/300 reqs") {
		t.Error("expected quota display in footer view when width allows")
	}
}
