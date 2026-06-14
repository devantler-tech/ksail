package main

import "testing"

const (
	binArg        = "ksail-desktop"
	resourcesLink = "ksail://resources"
	clusterLink   = "ksail://cluster/default/prod"
)

func TestFirstDeepLink(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{"no args", nil, "", false},
		{"no deep link", []string{binArg, "--flag"}, "", false},
		{"wrong scheme", []string{binArg, "https://example.com"}, "", false},
		{"present", []string{binArg, clusterLink}, clusterLink, true},
		{"among other args", []string{binArg, "--x", resourcesLink, "y"}, resourcesLink, true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, ok := firstDeepLink(testCase.args)
			if got != testCase.want || ok != testCase.ok {
				t.Errorf(
					"firstDeepLink(%v) = (%q, %v), want (%q, %v)",
					testCase.args, got, ok, testCase.want, testCase.ok,
				)
			}
		})
	}
}

func TestIsDeepLink(t *testing.T) {
	t.Parallel()

	matches := []string{"ksail://", clusterLink, resourcesLink}
	for _, raw := range matches {
		if !isDeepLink(raw) {
			t.Errorf("isDeepLink(%q) = false, want true", raw)
		}
	}

	nonMatches := []string{
		"", "https://example.com", "ksail:", "ksail:/x", "KSAIL://x", " ksail://x",
	}
	for _, raw := range nonMatches {
		if isDeepLink(raw) {
			t.Errorf("isDeepLink(%q) = true, want false", raw)
		}
	}
}
