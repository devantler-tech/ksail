package hetzner_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/require"
)

func TestValidateISOArchitecture(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name, isoType, isoArch, serverArch string
		valid                              bool
	}{
		{"matching x86", "public", `"x86"`, "x86", true},
		{"matching arm", "public", `"arm"`, "arm", true},
		{"incompatible", "public", `"x86"`, "arm", false},
		{"private (uploaded) wildcard", "private", `null`, "arm", true},
		{"missing public metadata", "public", `null`, "arm", false},
		{"unknown type is not a wildcard", "custom", `null`, "arm", false},
		{"missing target metadata", "public", `"x86"`, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(
				http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
					responseWriter.Header().Set("Content-Type", "application/json")

					if request.URL.Path == "/isos/42" {
						_, _ = fmt.Fprintf(
							responseWriter,
							`{"iso":{"id":42,"type":%q,"architecture":%s}}`,
							test.isoType,
							test.isoArch,
						)

						return
					}

					_, _ = fmt.Fprintf(
						responseWriter,
						`{"server_types":[{"id":1,"name":"target","architecture":%q}]}`,
						test.serverArch,
					)
				}),
			)
			t.Cleanup(server.Close)
			provider := hetzner.NewProvider(
				hcloud.NewClient(hcloud.WithToken("test"), hcloud.WithEndpoint(server.URL)),
			)

			err := provider.ValidateISO(t.Context(), 42, "target")
			if test.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, hetzner.ErrISOArchitectureMismatch)
			}
		})
	}
}
