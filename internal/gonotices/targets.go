package gonotices

import "slices"

// ValidateTargets closes recipient material coverage over all declared release graphs.
func ValidateTargets(targets []string) error {
	want := recipientTargets()
	actual := slices.Clone(targets)

	slices.Sort(want)
	slices.Sort(actual)

	if !slices.Equal(actual, want) {
		return ErrIncomplete
	}

	return nil
}

func recipientTargets() []string {
	return []string{
		"cli-darwin-arm64",
		"cli-linux-amd64",
		"cli-linux-arm64",
		"cli-windows-amd64",
		"cli-windows-arm64",
		"desktop-darwin-arm64",
		"desktop-linux-amd64",
		"desktop-windows-amd64",
	}
}
