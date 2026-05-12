package hetzner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serverTypeLocSchema mirrors the Hetzner API schema for a server type location.
type serverTypeLocSchema struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

// serverTypeSchema mirrors the Hetzner API schema for a server type.
type serverTypeSchema struct {
	ID        int64                 `json:"id"`
	Name      string                `json:"name"`
	Locations []serverTypeLocSchema `json:"locations"`
}

// serverTypeListResp mirrors the Hetzner API list response.
type serverTypeListResp struct {
	ServerTypes []serverTypeSchema `json:"server_types"` //nolint:tagliatelle // Hetzner API uses snake_case
}

// newAvailabilityTestServer creates a test HTTP server that responds to
// GET /server_types?name=... with the given server type schemas.
func newAvailabilityTestServer(t *testing.T, types map[string]serverTypeSchema) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /server_types", func(writer http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")

		resp := serverTypeListResp{}
		if serverType, ok := types[name]; ok {
			resp.ServerTypes = []serverTypeSchema{serverType}
		}

		writeJSONResponse(t, writer, resp)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

//nolint:funlen // Table-driven test with many cases
func TestCheckServerAvailability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		types             map[string]serverTypeSchema
		serverTypes       []string
		primaryLocation   string
		fallbackLocations []string
		wantErr           error
		wantErrContains   string
	}{
		{
			name: "AvailableInPrimary",
			types: map[string]serverTypeSchema{
				"cx22": {ID: 1, Name: "cx22", Locations: []serverTypeLocSchema{
					{ID: 1, Name: "fsn1", Available: true},
				}},
			},
			serverTypes:     []string{"cx22"},
			primaryLocation: "fsn1",
		},
		{
			name: "AvailableInFallback",
			types: map[string]serverTypeSchema{
				"cx22": {ID: 1, Name: "cx22", Locations: []serverTypeLocSchema{
					{ID: 1, Name: "fsn1", Available: false},
					{ID: 2, Name: "nbg1", Available: true},
				}},
			},
			serverTypes:       []string{"cx22"},
			primaryLocation:   "fsn1",
			fallbackLocations: []string{"nbg1"},
		},
		{
			name: "MultipleTypesAllAvailable",
			types: map[string]serverTypeSchema{
				"cx22": {ID: 1, Name: "cx22", Locations: []serverTypeLocSchema{
					{ID: 1, Name: "fsn1", Available: true},
				}},
				"cpx31": {ID: 2, Name: "cpx31", Locations: []serverTypeLocSchema{
					{ID: 1, Name: "fsn1", Available: true},
				}},
			},
			serverTypes:     []string{"cx22", "cpx31"},
			primaryLocation: "fsn1",
		},
		{
			name:            "ServerTypeNotFound",
			types:           map[string]serverTypeSchema{},
			serverTypes:     []string{"nonexistent"},
			primaryLocation: "fsn1",
			wantErr:         hetzner.ErrServerTypeNotFound,
		},
		{
			name: "UnavailableInAllLocations",
			types: map[string]serverTypeSchema{
				"cx22": {ID: 1, Name: "cx22", Locations: []serverTypeLocSchema{
					{ID: 1, Name: "fsn1", Available: false},
					{ID: 2, Name: "nbg1", Available: false},
				}},
			},
			serverTypes:       []string{"cx22"},
			primaryLocation:   "fsn1",
			fallbackLocations: []string{"nbg1"},
			wantErr:           hetzner.ErrServerTypeUnavailable,
		},
		{
			name: "UnavailableLocationNotListed",
			types: map[string]serverTypeSchema{
				"cx22": {ID: 1, Name: "cx22", Locations: []serverTypeLocSchema{
					{ID: 1, Name: "hel1", Available: true},
				}},
			},
			serverTypes:     []string{"cx22"},
			primaryLocation: "fsn1",
			wantErr:         hetzner.ErrServerTypeUnavailable,
			wantErrContains: "fsn1",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			srv := newAvailabilityTestServer(t, testCase.types)
			prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

			err := prov.CheckServerAvailability(
				context.Background(),
				testCase.serverTypes,
				testCase.primaryLocation,
				testCase.fallbackLocations,
			)

			if testCase.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, testCase.wantErr,
					"expected %v, got %v", testCase.wantErr, err)

				if testCase.wantErrContains != "" {
					assert.Contains(t, err.Error(), testCase.wantErrContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckServerAvailability_NilClient(t *testing.T) {
	t.Parallel()

	prov := hetzner.NewProvider(nil)

	err := prov.CheckServerAvailability(
		context.Background(),
		[]string{"cx22"},
		"fsn1",
		nil,
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

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
			name:  "SkipsEmpty",
			input: []string{"cx23", "", "cpx31"},
			want:  []string{"cx23", "cpx31"},
		},
		{
			name:  "SkipsWhitespace",
			input: []string{"cx23", "  ", "\t", "cpx31"},
			want:  []string{"cx23", "cpx31"},
		},
		{
			name:  "TrimsWhitespace",
			input: []string{" cx23 ", "cpx31"},
			want:  []string{"cx23", "cpx31"},
		},
		{
			name:  "AllEmpty",
			input: []string{"", " ", "\t"},
			want:  []string{},
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
		{
			name:      "EmptyPrimary",
			primary:   "",
			fallbacks: []string{"nbg1", "hel1"},
			want:      []string{"nbg1", "hel1"},
		},
		{
			name:      "DuplicateLocations",
			primary:   "fsn1",
			fallbacks: []string{"fsn1", "nbg1"},
			want:      []string{"fsn1", "nbg1"},
		},
		{
			name:      "WhitespaceFiltered",
			primary:   "  ",
			fallbacks: []string{"", "nbg1"},
			want:      []string{"nbg1"},
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
