package chat_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
)

// TestDefaultKeyMap tests that the default keymap is correctly configured.
func TestDefaultKeyMap(t *testing.T) {
	t.Parallel()

	keys := chat.DefaultKeyMap()

	// Verify ShortHelp returns bindings
	shortHelp := keys.ShortHelp()
	if len(shortHelp) == 0 {
		t.Error("expected non-empty ShortHelp bindings")
	}

	// Verify FullHelp returns grouped bindings
	fullHelp := keys.FullHelp()
	if len(fullHelp) == 0 {
		t.Error("expected non-empty FullHelp bindings")
	}

	// Verify PermissionShortHelp returns bindings
	permHelp := keys.PermissionShortHelp()
	if len(permHelp) == 0 {
		t.Error("expected non-empty PermissionShortHelp bindings")
	}

	// Verify PickerShortHelp returns bindings
	pickerHelp := keys.PickerShortHelp()
	if len(pickerHelp) == 0 {
		t.Error("expected non-empty PickerShortHelp bindings")
	}

	// Verify SessionPickerShortHelp returns bindings
	sessionHelp := keys.SessionPickerShortHelp()
	if len(sessionHelp) == 0 {
		t.Error("expected non-empty SessionPickerShortHelp bindings")
	}
}
