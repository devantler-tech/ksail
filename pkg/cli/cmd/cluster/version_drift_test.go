package cluster_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeVersionTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"adds v prefix", "1.13.0", "v1.13.0"},
		{"keeps existing v", "v1.13.0", "v1.13.0"},
		{"trims whitespace", "  1.13.0 ", "v1.13.0"},
		{"empty stays empty", "", ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, cluster.ExportNormalizeVersionTag(testCase.in))
		})
	}
}

func TestVersionsEqual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current string
		target  string
		want    bool
	}{
		{"identical", "v1.13.0", "v1.13.0", true},
		{"v-prefix mismatch is equal", "1.13.0", "v1.13.0", true},
		{"different patch", "v1.13.0", "v1.13.1", false},
		{"unparseable falls back to string compare", "weird", "weird", true},
		{"unparseable differing", "weird", "other", false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(
				t,
				testCase.want,
				cluster.ExportVersionsEqual(testCase.current, testCase.target),
			)
		})
	}
}

func TestIsDowngrade(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current string
		target  string
		want    bool
	}{
		{"older target is downgrade", "v1.13.0", "v1.12.0", true},
		{"newer target is not", "v1.12.0", "v1.13.0", false},
		{"equal is not a downgrade", "v1.13.0", "v1.13.0", false},
		{"unparseable is not flagged", "weird", "other", false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(
				t,
				testCase.want,
				cluster.ExportIsDowngrade(testCase.current, testCase.target),
			)
		})
	}
}
