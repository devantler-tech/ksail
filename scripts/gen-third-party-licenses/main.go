// Command gen-third-party-licenses regenerates the repo-root THIRD_PARTY_LICENSES
// inventory from the live module graphs of both Go modules (root + desktop/).
//
// It shells out to go-licenses (github.com/google/go-licenses/v2 — the same tool
// and version CI's license check installs) for the per-module license
// classification, merges the two module graphs, and emits one consolidated,
// deterministic document: a summary, then one section per license type with the
// module list and a representative license text read from the module cache.
//
// Modules go-licenses reports as "Unknown" (no license file in the module
// archive) must be manually verified and recorded in verified_unknown.go —
// the generator fails on any unverified Unknown module so the inventory can
// never silently carry an unreviewed dependency.
//
// Run via `make licenses` (or `go run ./scripts/gen-third-party-licenses`).
// The output contains no timestamp so a re-run with an unchanged module graph
// is byte-identical (CI relies on that for drift detection); regeneration
// history lives in git.
package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const ownModulePrefix = "github.com/devantler-tech/ksail"

// unknownLicense is the license type go-licenses reports when a module archive
// bundles no license file.
const unknownLicense = "Unknown"

// licenseFileMode is the permission mode for the generated inventory (git
// tracks only the executable bit, so owner-only write is fine).
const licenseFileMode = 0o600

var errUnverifiedUnknown = errors.New(
	"modules with no bundled license file and no manual verification " +
		"(verify each module's license against its source repository and add it to " +
		"scripts/gen-third-party-licenses/verified_unknown.go)")

var errNoLicenseFile = errors.New("no module license file found")

// generationTimeout bounds the whole run (go-licenses walks both module
// graphs and reads the module cache; a wedged subprocess must not hang a
// local `make licenses` forever — CI has its own job-level timeout).
const generationTimeout = 15 * time.Minute

type dependency struct {
	// module is the import path exactly as go-licenses csv emits it — for
	// modules whose license lives at a sub-package level this is a PACKAGE
	// import path (e.g. github.com/segmentio/asm/ascii), not the module root.
	module  string
	license string
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-third-party-licenses: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), generationTimeout)
	defer cancel()

	repoRoot, err := findRepoRoot(ctx)
	if err != nil {
		return err
	}

	moduleDirs := []string{repoRoot, filepath.Join(repoRoot, "desktop")}

	deps, err := collectDependencies(ctx, moduleDirs)
	if err != nil {
		return err
	}

	deps = applyOverrides(deps)

	err = checkUnknowns(deps)
	if err != nil {
		return err
	}

	texts, err := collectLicenseTexts(ctx, moduleDirs, deps)
	if err != nil {
		return err
	}

	outPath := filepath.Join(repoRoot, "THIRD_PARTY_LICENSES")

	err = os.WriteFile(outPath, []byte(render(deps, texts)), licenseFileMode)
	if err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}

	return nil
}

func findRepoRoot(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("locating repo root: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// collectDependencies runs `go-licenses csv ./...` in every module dir and
// merges the results into one module→license inventory, skipping the repo's
// own modules.
func collectDependencies(ctx context.Context, moduleDirs []string) ([]dependency, error) {
	byModule := map[string]string{}

	for _, dir := range moduleDirs {
		cmd := exec.CommandContext(ctx, "go-licenses", "csv", "./...")
		cmd.Dir = dir

		var stdout, stderr bytes.Buffer

		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err != nil {
			return nil, fmt.Errorf("go-licenses csv in %s: %w\n%s", dir, err, stderr.String())
		}

		err = mergeCSV(byModule, &stdout)
		if err != nil {
			return nil, fmt.Errorf("parsing go-licenses csv output from %s: %w", dir, err)
		}
	}

	deps := make([]dependency, 0, len(byModule))
	for module, license := range byModule {
		deps = append(deps, dependency{module: module, license: license})
	}

	sort.Slice(deps, func(left, right int) bool { return deps[left].module < deps[right].module })

	return deps, nil
}

// mergeCSV folds one go-licenses CSV stream (module,url,license) into the
// accumulated inventory. First writer wins so the root module's classification
// takes precedence over desktop's for shared dependencies.
func mergeCSV(byModule map[string]string, reader io.Reader) error {
	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = 3

	for {
		record, err := csvReader.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("reading csv record: %w", err)
		}

		module, license := record[0], record[2]
		if strings.HasPrefix(module, ownModulePrefix) {
			continue
		}

		_, seen := byModule[module]
		if !seen {
			byModule[module] = license
		}
	}

	return nil
}

// checkUnknowns fails when a module classified Unknown has no manually
// verified entry in verified_unknown.go.
func checkUnknowns(deps []dependency) error {
	verified := verifiedUnknown()

	var unverified []string

	for _, dep := range deps {
		if dep.license != unknownLicense {
			continue
		}

		_, ok := verified[dep.module]
		if !ok {
			unverified = append(unverified, dep.module)
		}
	}

	if len(unverified) == 0 {
		return nil
	}

	return fmt.Errorf("%w:\n  %s", errUnverifiedUnknown, strings.Join(unverified, "\n  "))
}

// collectLicenseTexts returns one representative license text per license
// type: the license file of the alphabetically-first module of that type
// (deterministic across runs), read from the module cache directory `go list`
// resolves for the package.
func collectLicenseTexts(
	ctx context.Context, moduleDirs []string, deps []dependency,
) (map[string]string, error) {
	texts := map[string]string{}

	for _, dep := range representativeModules(deps) {
		text, err := readModuleLicense(ctx, moduleDirs, dep.module)
		if err != nil {
			return nil, fmt.Errorf("license text for %s (%s): %w", dep.module, dep.license, err)
		}

		texts[dep.license] = text
	}

	return texts, nil
}

// representativeModules picks the alphabetically-first module per license type
// (excluding Unknown, whose section carries verification notes instead of a
// text). deps must already be sorted by module.
func representativeModules(deps []dependency) []dependency {
	seen := map[string]bool{}

	var reps []dependency

	for _, dep := range deps {
		if dep.license == unknownLicense || seen[dep.license] {
			continue
		}

		seen[dep.license] = true

		reps = append(reps, dep)
	}

	return reps
}

// readModuleLicense resolves the package's module directory (module cache)
// via `go list` in any of the module dirs and reads the license file found
// closest to the package: the package's own directory first, then each parent
// up to the module root (go-licenses classifies at the same granularity).
func readModuleLicense(ctx context.Context, moduleDirs []string, pkg string) (string, error) {
	for _, dir := range moduleDirs {
		// #nosec G204 -- pkg comes from go-licenses csv output over this repo's
		// own module graph, not from user input.
		cmd := exec.CommandContext(
			ctx, "go", "list", "-f", "{{if .Module}}{{.Module.Dir}}|{{.Dir}}{{end}}", pkg)
		cmd.Dir = dir

		out, err := cmd.Output()
		if err != nil {
			continue
		}

		moduleDir, pkgDir, ok := strings.Cut(strings.TrimSpace(string(out)), "|")
		if !ok || moduleDir == "" {
			continue
		}

		for candidate := pkgDir; strings.HasPrefix(candidate, moduleDir); candidate = filepath.Dir(candidate) {
			text, found := readLicenseFile(candidate)
			if found {
				return text, nil
			}
		}
	}

	return "", fmt.Errorf("%w: package %s", errNoLicenseFile, pkg)
}

// readLicenseFile returns the content of the alphabetically-first regular
// file in dir whose name looks like a license file.
func readLicenseFile(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}

	for _, entry := range entries {
		name := strings.ToUpper(entry.Name())
		if entry.IsDir() ||
			(!strings.Contains(name, "LICEN") && !strings.HasPrefix(name, "COPYING")) {
			continue
		}

		// #nosec G304 -- dir is a directory inside the local Go module cache,
		// resolved via `go list`; not user input.
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		return strings.TrimRight(string(data), "\n"), true
	}

	return "", false
}
