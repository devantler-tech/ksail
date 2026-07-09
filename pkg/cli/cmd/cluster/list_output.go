package cluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// ListItemJSON is the JSON representation of a single cluster row emitted by
// `cluster list --output json`. The shape is a stable contract consumed by the
// VS Code extension: a JSON array of these objects.
//
// Status is the cluster's run-state ("Running"/"Stopped"/"Unknown"), matching the
// human STATUS column — it lets consumers (the VS Code extension) read cluster
// status from this contract instead of sniffing `docker ps`.
//
// TTL is a pointer so it serialises to null when no TTL is set (and to the
// human-readable remaining duration, or "EXPIRED", otherwise).
type ListItemJSON struct {
	Name         string  `json:"name"`
	Provider     string  `json:"provider"`
	Distribution string  `json:"distribution"`
	Status       string  `json:"status"`
	TTL          *string `json:"ttl"`
}

// buildListJSON converts the ordered list results into the JSON contract rows,
// following the same provider ordering as the human table so both outputs agree.
func buildListJSON(providers []v1alpha1.Provider, results []listResult) []ListItemJSON {
	rows := make([]ListItemJSON, 0, len(results))

	for _, prov := range providers {
		for _, result := range results {
			if result.Provider != prov {
				continue
			}

			rows = append(rows, ListItemJSON{
				Name:         result.ClusterName,
				Provider:     strings.ToLower(string(result.Provider)),
				Distribution: string(result.Distribution),
				Status:       statusLabel(result.RunState),
				TTL:          listTTLValue(result),
			})
		}
	}

	// Unmanaged (kubeconfig-only) clusters have no provider, so the provider loop above skips them —
	// append them last with empty provider/distribution and status "Unmanaged".
	for _, result := range unmanagedResults(results) {
		rows = append(rows, ListItemJSON{
			Name:         result.ClusterName,
			Provider:     "",
			Distribution: "",
			Status:       statusLabel(result.RunState),
			TTL:          listTTLValue(result),
		})
	}

	return rows
}

// listTTLValue returns a pointer to the TTL display string, or nil when no TTL
// is set so the JSON field serialises to null.
func listTTLValue(result listResult) *string {
	ttlStr := formatTTLValue(result.TTL)
	if ttlStr == "" {
		return nil
	}

	return &ttlStr
}

// emitListJSON serialises the cluster list as an indented JSON array and writes
// it to the writer. An empty result set emits "[]" so consumers always parse a
// valid array.
func emitListJSON(writer io.Writer, providers []v1alpha1.Provider, results []listResult) error {
	rows := buildListJSON(providers, results)

	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// Keep '<', '>', '&' literal instead of \u-escaping them; this is CLI
	// output, not HTML.
	enc.SetEscapeHTML(false)

	err := enc.Encode(rows)
	if err != nil {
		return fmt.Errorf("marshal cluster list to JSON: %w", err)
	}

	// enc.Encode already appends a trailing newline.
	_, _ = fmt.Fprint(writer, buf.String())

	return nil
}
