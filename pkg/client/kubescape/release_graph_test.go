package kubescape_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// goListPackageFields limits `go list -json` to the fields goListPackage reads: the full record is
// about 69 MB per module and platform, and the audit lists ten of them.
const goListPackageFields = "ImportPath,Name,Dir,Standard,Module,GoFiles,CgoFiles,IgnoredGoFiles,Imports"

// goListPackage mirrors the Go command's exported JSON field names.
type goListPackage struct {
	ImportPath     string        `json:"ImportPath"`     //nolint:tagliatelle // Go command output contract.
	Name           string        `json:"Name"`           //nolint:tagliatelle // Go command output contract.
	Dir            string        `json:"Dir"`            //nolint:tagliatelle // Go command output contract.
	Standard       bool          `json:"Standard"`       //nolint:tagliatelle // Go command output contract.
	Module         *goListModule `json:"Module"`         //nolint:tagliatelle // Go command output contract.
	GoFiles        []string      `json:"GoFiles"`        //nolint:tagliatelle // Go command output contract.
	CgoFiles       []string      `json:"CgoFiles"`       //nolint:tagliatelle // Go command output contract.
	IgnoredGoFiles []string      `json:"IgnoredGoFiles"` //nolint:tagliatelle // Go command output contract.
	Imports        []string      `json:"Imports"`        //nolint:tagliatelle // Go command output contract.
}

// platform is one GOOS/GOARCH pair a module is built for.
type platform struct {
	goos   string
	goarch string
}

func (p platform) String() string { return p.goos + "/" + p.goarch }

// shippedPlatforms is every GOOS/GOARCH pair either module ships on: the CLI release matrix
// (.goreleaser.yaml: darwin, linux and windows on amd64 and arm64, less darwin/amd64), which also
// covers every desktop build (darwin/arm64 cask, linux/amd64 and windows/amd64 downloads). The
// package graph differs per platform, so a caller present only in, say, the windows/arm64 graph is
// invisible to any other listing. An unshipped pair is left out: its graph could fail the audit
// for a binary nobody receives. TestShippedPlatformsCoverReleaseMatrices keeps this list equal to
// the release matrices in both directions.
func shippedPlatforms() []platform {
	return []platform{
		{goos: "darwin", goarch: "arm64"},
		{goos: "linux", goarch: "amd64"},
		{goos: "linux", goarch: "arm64"},
		{goos: "windows", goarch: "amd64"},
		{goos: "windows", goarch: "arm64"},
	}
}

// moduleCGO is the CGO setting each shipped module is built with: the CLI release disables CGO,
// while the desktop app needs it for its webview, and without it the desktop graph cannot be listed.
func moduleCGO() map[string]string {
	return map[string]string{"root": "0", "desktop": "1"}
}

// moduleBuildTags is the build tag set each shipped build is compiled with: the desktop app shares
// the root module and opts in with the `desktop` tag (ADR 0007), so without it the desktop graph is
// the CLI's.
func moduleBuildTags() map[string]string {
	return map[string]string{"root": "", "desktop": "desktop"}
}

// auditGoEnv is the caller's environment with every graph-affecting Go setting the release
// configuration does not control cleared: GOFLAGS (tags, -modfile, -mod), GOEXPERIMENT (which adds
// goexperiment.* tags), the user go env file and a workspace file would otherwise make the audit
// list a graph the release never builds.
func auditGoEnv(extra ...string) []string {
	env := append(os.Environ(), "GOFLAGS=", "GOENV=off", "GOWORK=off", "GOEXPERIMENT=")

	return append(env, extra...)
}

func listDependencyPackages(
	t *testing.T,
	moduleDir string,
	target platform,
	cgo string,
	tags string,
) []goListPackage {
	t.Helper()

	//nolint:gosec // G204: a fixed go subcommand; tags come from moduleBuildTags.
	cmd := exec.CommandContext(
		t.Context(),
		"go",
		"list",
		"-deps",
		"-tags="+tags,
		"-json="+goListPackageFields,
		"./...",
	)
	cmd.Dir = moduleDir

	cmd.Env = auditGoEnv("GOOS="+target.goos, "GOARCH="+target.goarch, "CGO_ENABLED="+cgo)

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("list dependency packages for %s: %v\n%s", target, err,
			boundGoStderr(stderr.String(), goCommandStderrLimit))
	}

	var packages []goListPackage

	decoder := json.NewDecoder(bytes.NewReader(out))

	for {
		var pkg goListPackage

		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("decode go list output for %q: %v", moduleDir, err)
		}

		packages = append(packages, pkg)
	}

	return packages
}

// releaseTarget is one GOOS/GOARCH pair a release configuration builds.
type releaseTarget struct {
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
}

// TestShippedPlatformsCoverReleaseMatrices pins shippedPlatforms to the release configurations: every
// target the CLI release (.goreleaser.yaml), the macOS desktop release (.goreleaser.desktop.yaml) and
// the Linux/Windows desktop builds (cd.yaml) produce must be audited, so a target added to a release
// cannot ship a graph the release-graph audit never listed.
func TestShippedPlatformsCoverReleaseMatrices(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	audited := map[string]bool{}

	for _, target := range shippedPlatforms() {
		audited[target.String()] = true
	}

	targets := append(goreleaserTargets(t, filepath.Join(root, ".goreleaser.yaml")),
		goreleaserTargets(t, filepath.Join(root, ".goreleaser.desktop.yaml"))...)
	targets = append(
		targets,
		desktopWorkflowTargets(t, filepath.Join(root, ".github", "workflows", "cd.yaml"))...)

	if len(targets) == 0 {
		t.Fatal("no release target was read, so the comparison examined nothing")
	}

	released := map[string]bool{}

	for _, target := range targets {
		key := target.GOOS + "/" + target.GOARCH
		released[key] = true

		if !audited[key] {
			t.Errorf("release target %s is not in shippedPlatforms (release-graph audit)", key)
		}
	}

	for key := range audited {
		if !released[key] {
			t.Errorf(
				"shippedPlatforms audits %s, which no release builds (release-graph audit)",
				key,
			)
		}
	}
}

// goreleaserTargets expands every build's goos x goarch matrix in a GoReleaser file, less its ignores.
func goreleaserTargets(t *testing.T, path string) []releaseTarget {
	t.Helper()

	var config struct {
		Builds []struct {
			GOOS   []string        `json:"goos"`
			GOARCH []string        `json:"goarch"`
			Ignore []releaseTarget `json:"ignore"`
		} `json:"builds"`
	}

	readYAML(t, path, &config)

	var targets []releaseTarget

	for _, build := range config.Builds {
		if len(build.GOOS) == 0 || len(build.GOARCH) == 0 {
			t.Fatalf("%s has a build without an explicit goos and goarch list", path)
		}

		for _, goos := range build.GOOS {
			for _, goarch := range build.GOARCH {
				target := releaseTarget{GOOS: goos, GOARCH: goarch}
				if !slices.Contains(build.Ignore, target) {
					targets = append(targets, target)
				}
			}
		}
	}

	return targets
}

// desktopWorkflowTargets reads the desktop job's build matrix from the CD workflow.
func desktopWorkflowTargets(t *testing.T, path string) []releaseTarget {
	t.Helper()

	var workflow struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Include []struct {
						GOOS string `json:"goos"`
						Arch string `json:"arch"`
					} `json:"include"`
				} `json:"matrix"`
			} `json:"strategy"`
		} `json:"jobs"`
	}

	readYAML(t, path, &workflow)

	include := workflow.Jobs["desktop"].Strategy.Matrix.Include
	if len(include) == 0 {
		t.Fatalf("%s has no desktop build matrix", path)
	}

	targets := make([]releaseTarget, 0, len(include))
	for _, entry := range include {
		targets = append(targets, releaseTarget{GOOS: entry.GOOS, GOARCH: entry.Arch})
	}

	return targets
}

func readYAML(t *testing.T, path string, into any) {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // G304: fixed release configuration paths.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	err = yaml.Unmarshal(data, into)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

// TestModuleCGOMatchesReleaseBuilds pins moduleCGO to the CGO_ENABLED each module's GoReleaser
// configuration builds with: a file selected only by the other CGO mode belongs to a graph the
// audit would otherwise never list. The Linux and Windows desktop builds in cd.yaml are native
// `go build` runs, where Go's own default is CGO enabled, matching the desktop setting here.
func TestModuleCGOMatchesReleaseBuilds(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	for name, file := range map[string]string{"root": ".goreleaser.yaml", "desktop": ".goreleaser.desktop.yaml"} {
		released := goreleaserCGO(t, filepath.Join(root, file))
		if moduleCGO()[name] != released {
			t.Errorf(
				"moduleCGO()[%q] = %q, but %s builds with CGO_ENABLED=%s (release-graph audit)",
				name,
				moduleCGO()[name],
				file,
				released,
			)
		}
	}
}

// TestModuleBuildTagsMatchReleaseBuilds pins moduleBuildTags to the build tags each GoReleaser
// configuration compiles with: a tag-gated file belongs to a graph the audit would otherwise never
// list.
func TestModuleBuildTagsMatchReleaseBuilds(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	for name, file := range map[string]string{"root": ".goreleaser.yaml", "desktop": ".goreleaser.desktop.yaml"} {
		released := goreleaserTags(t, filepath.Join(root, file))
		if moduleBuildTags()[name] != released {
			t.Errorf("moduleBuildTags()[%q] = %q, but %s builds with tags %q (release-graph audit)",
				name, moduleBuildTags()[name], file, released)
		}
	}
}

// goreleaserTags returns the comma-joined build tags every build in a GoReleaser file sets; builds
// that disagree fail the test rather than guess.
func goreleaserTags(t *testing.T, path string) string {
	t.Helper()

	var config struct {
		Builds []struct {
			Tags []string `json:"tags"`
		} `json:"builds"`
	}

	readYAML(t, path, &config)

	if len(config.Builds) == 0 {
		t.Fatalf("%s has no build", path)
	}

	value := strings.Join(config.Builds[0].Tags, ",")

	for _, build := range config.Builds[1:] {
		if joined := strings.Join(build.Tags, ","); joined != value {
			t.Fatalf("%s: every build must set the same tags, got %q and %q", path, value, joined)
		}
	}

	return value
}

// goreleaserCGO returns the CGO_ENABLED value every build in a GoReleaser file sets; a build that
// sets none, or builds that disagree, fail the test rather than guess.
func goreleaserCGO(t *testing.T, path string) string {
	t.Helper()

	var config struct {
		Builds []struct {
			Env []string `json:"env"`
		} `json:"builds"`
	}

	readYAML(t, path, &config)

	value := ""

	for _, build := range config.Builds {
		found := ""

		for _, entry := range build.Env {
			if cgo, ok := strings.CutPrefix(entry, "CGO_ENABLED="); ok {
				found = cgo
			}
		}

		if found == "" || (value != "" && found != value) {
			t.Fatalf(
				"%s: every build must set one CGO_ENABLED value, got %q after %q",
				path,
				found,
				value,
			)
		}

		value = found
	}

	if value == "" {
		t.Fatalf("%s has no build", path)
	}

	return value
}

// auditedReleaseConfigDigests pins the release build configuration the release-graph audit was
// established against: both GoReleaser files, the desktop job in cd.yaml and the setup action it
// uses. The platform and CGO tests above check what those files say today; this catches every
// other way a release can change the graph it ships (build tags, flags, a build dir pointing at
// another module, a CGO override inside a run line), because ANY change to them re-opens the audit.
// To update: re-run the release-graph audit for the new configuration, then paste the new digests.
//
//nolint:lll // file-path keys and 64-character SHA-256 digests do not wrap
func auditedReleaseConfigDigests() map[string]string {
	return map[string]string{
		".goreleaser.yaml":                                   "603b04ca07558b8e1acb3d9c4e00e3c49ceaa10ea9f5af0ed2f84f01f96496c2",
		".goreleaser.desktop.yaml":                           "46eef12c0c592f5fae1a76082d897a1f63099a4a87b268ed7a39629c8e7c4eb6",
		".github/actions/setup-desktop-build/action.yml":     "09d319886697e84b880a9744cacb0928daa9a6998f24600cf09183e152592c6a",
		"scripts/stage-webui.sh":                             "5b3d7b0fa8b237f77ee9a88e6807e070c1b35c97df3d5f17357b3ecb8b2938a6",
		".github/actions/free-disk-space/free-disk-space.sh": "2dd12fcf3779137ca1cb5f21194947f6a10d3f94f8fb438a1a29ec9021fbdc30",
		".github/workflows/cd.yaml#without-uses":             "6ace111a92ab9ede90d87c45f537e5031f7abcae9f3b1b670bad6dd3cb2d62c6",
	}
}

// TestReleaseBuildConfigMatchesAudit fails when any audited release build configuration changes.
func TestReleaseBuildConfigMatchesAudit(t *testing.T) {
	t.Parallel()

	actual := releaseConfigDigests(t, moduleRoot(t))
	audited := auditedReleaseConfigDigests()

	// A hashed input without an audited digest would otherwise never be compared.
	for name := range actual {
		if _, ok := audited[name]; !ok {
			t.Errorf("%s is hashed but has no audited digest in auditedReleaseConfigDigests", name)
		}
	}

	for name, want := range audited {
		if actual[name] != want {
			t.Errorf(
				"%s changed (digest %s, audited %s): re-establish the GHSA-hjf4-fphr-2h65 verdict "+
					"for the new release configuration (release-graph audit), then update auditedReleaseConfigDigests",
				name,
				actual[name],
				want,
			)
		}
	}
}

// releaseConfigDigests hashes each audited release configuration: whole files, and for cd.yaml the
// whole workflow (workflow-level env such as GOFLAGS reaches every release job) canonicalised through
// JSON with every `uses:` value removed, so formatting and action-version bumps do not count.
func releaseConfigDigests(t *testing.T, root string) map[string]string {
	t.Helper()

	digests := map[string]string{}

	for _, name := range []string{
		".goreleaser.yaml", ".goreleaser.desktop.yaml", ".github/actions/setup-desktop-build/action.yml",
		"scripts/stage-webui.sh",
		".github/actions/free-disk-space/free-disk-space.sh",
	} {
		path := filepath.Join(root, name)

		data, err := os.ReadFile(path) //nolint:gosec // G304: fixed release config paths.
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		// A CRLF checkout (Windows, core.autocrlf) must not change the digest of unchanged content.
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
		digests[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}

	var workflow map[string]any

	readYAML(t, filepath.Join(root, ".github", "workflows", "cd.yaml"), &workflow)

	if _, ok := workflow["jobs"]; !ok {
		t.Fatal("cd.yaml has no jobs")
	}

	encoded, err := json.Marshal(withoutActionPins(workflow))
	if err != nil {
		t.Fatalf("encode cd.yaml: %v", err)
	}

	digests[".github/workflows/cd.yaml#without-uses"] = fmt.Sprintf("%x", sha256.Sum256(encoded))

	return digests
}

// withoutActionPins returns a copy of a decoded workflow with only the VERSION of each remote action
// removed (`owner/action@sha` becomes `owner/action`): a version bump cannot change the Go graph a
// release builds, while swapping in another action, or pointing at another local action, can.
func withoutActionPins(node any) any {
	switch value := node.(type) {
	case map[string]any:
		copied := make(map[string]any, len(value))

		for key, child := range value {
			if action, isString := child.(string); key == "uses" && isString {
				copied[key] = actionIdentity(action)

				continue
			}

			copied[key] = withoutActionPins(child)
		}

		return copied
	case []any:
		copied := make([]any, len(value))
		for index, child := range value {
			copied[index] = withoutActionPins(child)
		}

		return copied
	default:
		return node
	}
}

// actionIdentity keeps a local action's path whole and drops a remote action's @version.
func actionIdentity(action string) string {
	if strings.HasPrefix(action, "./") {
		return action
	}

	name, _, _ := strings.Cut(action, "@")

	return name
}
