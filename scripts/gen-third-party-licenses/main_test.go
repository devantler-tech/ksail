package main

import (
	"strings"
	"testing"
)

func TestMergeCSVSkipsOwnModulesAndDedupes(t *testing.T) {
	t.Parallel()

	byModule := map[string]string{}

	root := "github.com/foo/bar,https://example.com,MIT\n" +
		"github.com/devantler-tech/ksail/v7/pkg/cli,https://example.com,Unknown\n" +
		"github.com/baz/qux,https://example.com,Apache-2.0\n"

	err := mergeCSV(byModule, strings.NewReader(root))
	if err != nil {
		t.Fatalf("mergeCSV(root): %v", err)
	}

	desktop := "github.com/foo/bar,https://example.com,Unknown\n" +
		"github.com/devantler-tech/ksail/desktop,https://example.com,Unknown\n" +
		"github.com/desktop/only,https://example.com,MIT\n"

	err = mergeCSV(byModule, strings.NewReader(desktop))
	if err != nil {
		t.Fatalf("mergeCSV(desktop): %v", err)
	}

	want := map[string]string{
		"github.com/foo/bar":      "MIT", // root's classification wins
		"github.com/baz/qux":      "Apache-2.0",
		"github.com/desktop/only": "MIT",
	}
	if len(byModule) != len(want) {
		t.Fatalf("got %d modules, want %d: %v", len(byModule), len(want), byModule)
	}

	for module, license := range want {
		if byModule[module] != license {
			t.Errorf("byModule[%q] = %q, want %q", module, byModule[module], license)
		}
	}
}

func TestCheckUnknownsFailsOnUnverifiedModule(t *testing.T) {
	t.Parallel()

	deps := []dependency{
		{module: "github.com/not/verified", license: "Unknown"},
		{module: "github.com/segmentio/asm/ascii", license: "Unknown"}, // verified
		{module: "github.com/fine/mit", license: "MIT"},
	}

	err := checkUnknowns(deps)
	if err == nil {
		t.Fatal("checkUnknowns() = nil, want error for unverified module")
	}

	if !strings.Contains(err.Error(), "github.com/not/verified") {
		t.Errorf("error %q does not name the unverified module", err)
	}

	if strings.Contains(err.Error(), "segmentio") {
		t.Errorf("error %q names an already-verified module", err)
	}
}

func TestApplyOverridesElectsDualLicense(t *testing.T) {
	t.Parallel()

	deps := applyOverrides([]dependency{
		{module: "github.com/spdx/tools-golang", license: "GPL-2.0"},
		{module: "github.com/foo/bar", license: "MIT"},
	})

	if deps[0].license != "Apache-2.0" {
		t.Errorf("spdx/tools-golang license = %q, want elected Apache-2.0", deps[0].license)
	}

	if deps[1].license != "MIT" {
		t.Errorf("unrelated module license = %q, want MIT untouched", deps[1].license)
	}
}

func TestRepresentativeModulesPicksFirstPerLicense(t *testing.T) {
	t.Parallel()

	reps := representativeModules([]dependency{
		{module: "github.com/a/first", license: "MIT"},
		{module: "github.com/b/second", license: "MIT"},
		{module: "github.com/c/apache", license: "Apache-2.0"},
		{module: "github.com/d/unknown", license: "Unknown"},
	})

	if len(reps) != 2 {
		t.Fatalf("got %d representatives, want 2: %v", len(reps), reps)
	}

	if reps[0].module != "github.com/a/first" || reps[1].module != "github.com/c/apache" {
		t.Errorf("representatives = %v, want first module per non-Unknown license", reps)
	}
}

func TestRenderShapeAndDeterminism(t *testing.T) {
	t.Parallel()

	deps := applyOverrides([]dependency{
		{module: "github.com/a/mit", license: "MIT"},
		{module: "github.com/segmentio/asm/ascii", license: "Unknown"},
		{module: "github.com/spdx/tools-golang", license: "GPL-2.0"},
	})
	texts := map[string]string{"MIT": "MIT LICENSE TEXT", "Apache-2.0": "APACHE TEXT"}

	doc := render(deps, texts)

	for _, want := range []string{
		"LICENSE SUMMARY",
		"  Apache-2.0: 1 module(s)",
		"  MIT: 1 module(s)",
		"  Unknown (verified, see notes): 1 module(s)",
		"  Total: 3 module(s)",
		"LICENSE ELECTIONS AND CLASSIFICATION NOTES",
		"Apache-2.0 elected",
		"License: MIT",
		"MIT LICENSE TEXT",
		"License: Unknown (license file not bundled in Go module)",
		"Verified: MIT (https://github.com/segmentio/asm)",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("rendered document missing %q", want)
		}
	}

	if strings.Contains(doc, "License: GPL-2.0") {
		t.Error("dual-licensed module rendered under GPL-2.0 instead of its election")
	}

	if doc != render(deps, texts) {
		t.Error("render is not deterministic for identical input")
	}
}
