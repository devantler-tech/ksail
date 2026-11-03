package gonotices_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
	"github.com/stretchr/testify/require"
)

func TestMaterialDecisionBindsActualSourceDeclarationAndScopeEvidence(t *testing.T) {
	t.Parallel()
	pkg, policy := materialRuleFixture(t)
	resolved, err := policy.Resolve(pkg, "LICENSE.extra", []string{"Unknown"}, []string{"MIT"})
	require.NoError(t, err)
	require.Equal(t, "LICENSE.extra", resolved.Path)
	require.Equal(t, []string{"Unknown"}, resolved.Raw)
	require.Equal(t, []string{"MIT"}, resolved.Effective)
}

func TestMaterialDecisionCannotMaskAnotherSourceOrObligation(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{
		"version", "module", "sum", "replacement", "package", "path", "hash", "evidence",
		"detection", "denied", "unknown", "new-file", "duplicate", "embedded-exclusion",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			pkg, policy := materialRuleFixture(t)
			alterMaterialSource(&pkg, scenario)
			path, detected := alterMaterialObligation(t, &pkg, &policy, scenario)

			_, err := policy.Resolve(pkg, path, detected, []string{"MIT"})
			require.ErrorIs(t, err, gonotices.ErrIncomplete)
		})
	}
}

func alterMaterialSource(pkg *gonotices.Package, scenario string) {
	switch scenario {
	case "version":
		pkg.Module.Version = "v1.2.4"
	case "module":
		pkg.Module.Path = "example.com/another"
	case "sum":
		pkg.Module.Sum += "different"
	case "replacement":
		pkg.Module.Replace = &gonotices.Module{
			Path:      pkg.Module.Path,
			Version:   pkg.Module.Version,
			Sum:       pkg.Module.Sum,
			Directory: pkg.Module.Directory,
		}
	case "package":
		pkg.ImportPath += "/another"
	}
}

func alterMaterialObligation(
	t *testing.T,
	pkg *gonotices.Package,
	policy *gonotices.MaterialPolicy,
	scenario string,
) (string, []string) {
	t.Helper()

	path, detected := "LICENSE.extra", []string{"Unknown"}

	switch scenario {
	case "path":
		path = "../LICENSE.extra"
	case "hash":
		writeNoticeFixture(t, pkg.Module.Directory, path, "Different license obligation\n")
	case "evidence":
		writeNoticeFixture(
			t,
			pkg.Module.Directory,
			"README.md",
			"Different scope declaration\n",
		)
	case "detection":
		detected = []string{"GPL-3.0"}
	case "denied":
		policy.Files[0].Terms = []string{"GPL-3.0"}
	case "unknown":
		policy.Files[0].Terms = []string{"Unknown"}
	case "new-file":
		path = "COPYING.new"
		writeNoticeFixture(t, pkg.Module.Directory, path, "Unknown new declaration\n")
	case "duplicate":
		policy.Files = append(policy.Files, policy.Files[0])
	case "embedded-exclusion":
		policy.Files[0].Role = "excluded"
		policy.Files[0].Terms = nil
		policy.Files[0].NoEmbeddedFiles = true
		pkg.EmbedFiles = []string{"README.md"}
	}

	return path, detected
}

func TestMaterialRolesCannotInventAnAdditionalPackageGrant(t *testing.T) {
	t.Parallel()

	for _, role := range []string{"attribution", "excluded"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			pkg, policy := materialRuleFixture(t)
			policy.Files[0].Role, policy.Files[0].Terms = role, nil
			policy.Files[0].NoEmbeddedFiles = role == "excluded"
			declaration, err := policy.Resolve(
				pkg,
				"LICENSE.extra",
				[]string{"Unknown"},
				[]string{"MIT"},
			)
			require.NoError(t, err)
			require.Equal(t, role, declaration.Role)
			require.Empty(t, declaration.Effective)
			require.Equal(
				t,
				[]string{"MIT"},
				pkg.Effective,
				"recipient decisions must not modify package admission",
			)
		})
	}
}

func TestLoadMaterialPolicyRefusesMalformedOrAmbiguousFacts(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{
		"valid", "prettier", "missing", "duplicate-key", "nested-duplicate-key", "escaped-key",
		"unknown-key", "trailing", "private",
		"duplicate-rule", "empty-basis", "invalid-hash", "traversal",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			_, policy := materialRuleFixture(t)

			alterMaterialPolicy(&policy, scenario)
			data, err := json.MarshalIndent(policy, "", "  ")
			require.NoError(t, err)

			data = alterMaterialPolicyEncoding(append(data, '\n'), scenario)

			root := t.TempDir()
			if scenario != "missing" {
				writeNoticeFixture(t, root, ".github/go-notice-materials.json", string(data))
			}

			loaded, err := gonotices.LoadMaterialPolicy(root)
			if scenario == "valid" || scenario == "prettier" {
				require.NoError(t, err)
				require.Equal(t, policy, loaded)
			} else {
				require.ErrorIs(t, err, gonotices.ErrIncomplete)
			}
		})
	}
}

func alterMaterialPolicy(policy *gonotices.MaterialPolicy, scenario string) {
	switch scenario {
	case "duplicate-rule":
		policy.Files = append(policy.Files, policy.Files[0])
	case "empty-basis":
		policy.Files[0].Evidence = nil
	case "invalid-hash":
		policy.Files[0].SHA256 = strings.Repeat("z", 64)
	case "traversal":
		policy.Files[0].Evidence[0].Path = "../outside"
	case "private":
		policy.Files[0].Reason = "/private/tmp/private-correspondence/never-publish"
	}
}

func alterMaterialPolicyEncoding(data []byte, scenario string) []byte {
	switch scenario {
	case "prettier":
		// Native Prettier keeps singleton scalar arrays on one line.
		data = []byte(
			strings.ReplaceAll(string(data), "[\n        \"Unknown\"\n      ]", `["Unknown"]`),
		)
	case "duplicate-key":
		data = []byte(
			strings.Replace(
				string(data),
				"\"schema\": 1,",
				"\"schema\": 1,\n  \"schema\": 1,",
				1,
			),
		)
	case "nested-duplicate-key":
		data = []byte(strings.Replace(string(data), `"role": "license",`,
			`"role": "license", "role": "license",`, 1))
	case "escaped-key":
		data = []byte(strings.Replace(string(data), `"schema": 1,`, `"\u0073chema": 1,`, 1))
	case "unknown-key":
		data = []byte(
			strings.Replace(
				string(data),
				"\"schema\": 1,",
				"\"unknown\": true,\n  \"schema\": 1,",
				1,
			),
		)
	case "trailing":
		data = append(data, []byte("{}")...)
	}

	return data
}

func materialRuleFixture(t *testing.T) (gonotices.Package, gonotices.MaterialPolicy) {
	t.Helper()
	input, directory := noticeInputFixture(t)
	pkg := input.Packages[0]
	pkg.Module = gonotices.Module{
		Path:      "example.com/component",
		Version:   "v1.2.3",
		Sum:       "h1:exact-source",
		Directory: directory,
	}
	writeNoticeFixture(t, directory, "LICENSE.extra", "Exact full MIT declaration fixture\n")
	writeNoticeFixture(
		t,
		directory,
		"README.md",
		"This declaration applies to the selected source\n",
	)

	fileHash := func(path string) string {
		t.Helper()

		data := readNoticeFixture(t, directory, path)
		sum := sha256.Sum256(data)

		return hex.EncodeToString(sum[:])
	}

	return pkg, gonotices.MaterialPolicy{Schema: 1, Files: []gonotices.MaterialRule{
		{
			Module:  pkg.Module.Path,
			Version: pkg.Module.Version,
			Sum:     pkg.Module.Sum,
			Path:    "LICENSE.extra",
			SHA256:  fileHash("LICENSE.extra"),
			Packages: []string{
				pkg.ImportPath,
			},
			Detected: []string{"Unknown"},
			Terms:    []string{"MIT"},
			Role:     "license",
			Evidence: []gonotices.MaterialEvidence{
				{Path: "README.md", SHA256: fileHash("README.md")},
			},
			Reason: "Exact primary-source material fixture",
		},
	}}
}
