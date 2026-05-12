package hetzner_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
)

func TestAvailableLocations(t *testing.T) {
	t.Parallel()

	serverType := &hcloud.ServerType{
		Locations: []hcloud.ServerTypeLocation{
			{Location: &hcloud.Location{Name: "fsn1"}, Available: true},
			{Location: &hcloud.Location{Name: "nbg1"}, Available: false},
			{Location: &hcloud.Location{Name: "hel1"}, Available: true},
		},
	}

	tests := []struct {
		name       string
		candidates []string
		want       []string
	}{
		{
			name:       "AllAvailable",
			candidates: []string{"fsn1", "hel1"},
			want:       []string{"fsn1", "hel1"},
		},
		{
			name:       "PrimaryAvailable",
			candidates: []string{"fsn1"},
			want:       []string{"fsn1"},
		},
		{
			name:       "OnlyFallbackAvailable",
			candidates: []string{"nbg1", "hel1"},
			want:       []string{"hel1"},
		},
		{
			name:       "NoneAvailable",
			candidates: []string{"nbg1"},
			want:       nil,
		},
		{
			name:       "UnknownLocation",
			candidates: []string{"ash1"},
			want:       nil,
		},
		{
			name:       "EmptyCandidates",
			candidates: []string{},
			want:       nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := hetzner.AvailableLocationsForTest(serverType, testCase.candidates)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestDeduplicateServerTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "NoDuplicates",
			input: []string{"cx23", "cpx31"},
			want:  []string{"cx23", "cpx31"},
		},
		{
			name:  "WithDuplicates",
			input: []string{"cx23", "cpx31", "cx23"},
			want:  []string{"cx23", "cpx31"},
		},
		{
			name:  "AllSame",
			input: []string{"cx23", "cx23", "cx23"},
			want:  []string{"cx23"},
		},
		{
			name:  "Empty",
			input: []string{},
			want:  []string{},
		},
		{
			name:  "Single",
			input: []string{"cx23"},
			want:  []string{"cx23"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := hetzner.DeduplicateServerTypesForTest(testCase.input)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestBuildLocationList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		primary   string
		fallbacks []string
		want      []string
	}{
		{
			name:      "PrimaryOnly",
			primary:   "fsn1",
			fallbacks: nil,
			want:      []string{"fsn1"},
		},
		{
			name:      "PrimaryWithFallbacks",
			primary:   "fsn1",
			fallbacks: []string{"nbg1", "hel1"},
			want:      []string{"fsn1", "nbg1", "hel1"},
		},
		{
			name:      "EmptyFallbacks",
			primary:   "fsn1",
			fallbacks: []string{},
			want:      []string{"fsn1"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := hetzner.BuildLocationListForTest(testCase.primary, testCase.fallbacks)
			assert.Equal(t, testCase.want, got)
		})
	}
}
