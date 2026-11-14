package gonotices_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
	"github.com/stretchr/testify/require"
)

func TestManifestPoolsFactsWithoutLosingPackageFileOwnership(t *testing.T) {
	t.Parallel()
	input, directory := noticeInputFixture(t)
	writeNoticeFixture(
		t,
		directory,
		"nested/nested.go",
		"// Copyright 2025 Selected Authors\npackage nested\n",
	)

	nested := input.Packages[0]
	nested.ImportPath += "/nested"
	nested.Directory = filepath.Join(directory, "nested")
	nested.GoFiles = []string{"nested.go"}
	input.Packages = append(input.Packages, nested)
	payload, err := gonotices.Build(input)
	require.NoError(t, err)

	var metadata manifestOwnershipFixture
	require.NoError(t, json.Unmarshal(payload.Manifest, &metadata))
	require.Equal(t, 2, metadata.Schema)
	require.Len(t, metadata.Sources, 1)
	require.Len(
		t,
		metadata.Materials,
		2,
		"identical license and copyright bytes retain their distinct kinds without per-file duplication",
	)
	require.Len(t, metadata.Packages, 2)
	require.Equal(t, metadata.Packages[0].Source, metadata.Packages[1].Source)
	require.Equal(t, ".", metadata.Packages[0].Directory)
	require.Equal(t, "nested", metadata.Packages[1].Directory)
	require.Equal(t, []string{"source.go"}, metadata.Packages[0].Files)
	require.Equal(t, []string{"nested.go"}, metadata.Packages[1].Files)
	require.Equal(t, "nested/nested.go", metadata.Packages[1].Materials[1].Path)
	require.NoError(t, gonotices.Write(input.Root, input.Target, payload))
	require.NoError(t, gonotices.Verify(input.Root, input.Target, payload))
	input.Target = "cli-windows-amd64"
	other, err := gonotices.Build(input)
	require.NoError(t, err)

	var otherMetadata map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(other.Manifest, &otherMetadata))

	var original map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(payload.Manifest, &original))
	require.JSONEq(t, string(original["sources"]), string(otherMetadata["sources"]))
	require.JSONEq(t, string(original["materials"]), string(otherMetadata["materials"]))
}

func TestCompactManifestRefusesUnexaminedGraphReferences(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{
		"unknown-source", "unknown-material", "duplicate-file", "traversal-file", "duplicate-package",
		"orphan-source", "orphan-material",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			input, _ := noticeInputFixture(t)
			payload, err := gonotices.Build(input)
			require.NoError(t, err)
			payload.Manifest = mutateCompactGraph(t, payload.Manifest, scenario)
			require.ErrorIs(
				t,
				gonotices.Write(input.Root, input.Target, payload),
				gonotices.ErrIncomplete,
			)
		})
	}
}

type compactEnvelopeFixture struct {
	Schema     int                        `json:"schema"`
	Scope      string                     `json:"scope"`
	Target     string                     `json:"target"`
	TextSHA256 string                     `json:"textSha256"`
	Inputs     json.RawMessage            `json:"inputs"`
	Sources    map[string]json.RawMessage `json:"sources"`
	Materials  map[string]json.RawMessage `json:"materials"`
	Packages   []json.RawMessage          `json:"packages"`
	Toolchain  json.RawMessage            `json:"toolchain,omitempty"`
}

func mutateCompactGraph(t *testing.T, data []byte, scenario string) []byte {
	t.Helper()

	var wire compactEnvelopeFixture
	require.NoError(t, json.Unmarshal(data, &wire))

	for key := range wire.Sources {
		if scenario == "unknown-source" {
			wire.Packages[0] = bytes.Replace(
				wire.Packages[0],
				[]byte(key),
				[]byte("s0000000000000000"),
				1,
			)
		}
	}

	for key := range wire.Materials {
		if scenario == "unknown-material" {
			wire.Packages[0] = bytes.ReplaceAll(
				wire.Packages[0],
				[]byte(key),
				[]byte("m0000000000000000"),
			)
		}
	}

	switch scenario {
	case "duplicate-file":
		wire.Packages[0] = bytes.Replace(
			wire.Packages[0],
			[]byte(`"files":["source.go"]`),
			[]byte(`"files":["source.go","source.go"]`),
			1,
		)
	case "traversal-file":
		wire.Packages[0] = bytes.Replace(
			wire.Packages[0],
			[]byte(`"files":["source.go"]`),
			[]byte(`"files":["../source.go"]`),
			1,
		)
	case "duplicate-package":
		wire.Packages = append(wire.Packages, wire.Packages[0])
	case "orphan-source":
		addOrphanFact(t, wire.Sources, "s", `"third_party/example"`, `"third_party/unlinked"`)
	case "orphan-material":
		addOrphanFact(t, wire.Materials, "m", `"kind":"license"`, `"kind":"notice"`)
	}

	encoded, err := json.Marshal(wire)
	require.NoError(t, err)
	require.NotEqual(
		t,
		string(data),
		string(append(encoded, '\n')),
		"control did not alter canonical producer data",
	)

	return append(encoded, '\n')
}

func addOrphanFact(
	t *testing.T,
	facts map[string]json.RawMessage,
	prefix, original, replacement string,
) {
	t.Helper()

	for _, fact := range facts {
		changed := bytes.Replace(fact, []byte(original), []byte(replacement), 1)
		if bytes.Equal(fact, changed) {
			continue
		}

		hash := sha256.Sum256(changed)
		facts[prefix+hex.EncodeToString(hash[:])[:16]] = changed

		return
	}

	t.Fatal("control could not create a valid unreferenced fact")
}

type manifestOwnershipFixture struct {
	Schema    int                        `json:"schema"`
	Sources   map[string]json.RawMessage `json:"sources"`
	Materials map[string]json.RawMessage `json:"materials"`
	Packages  []struct {
		Name      string   `json:"name"`
		Source    string   `json:"source"`
		Directory string   `json:"directory"`
		Files     []string `json:"files"`
		Materials []struct {
			Path     string `json:"path"`
			Material string `json:"material"`
		} `json:"materials"`
	} `json:"packages"`
}
