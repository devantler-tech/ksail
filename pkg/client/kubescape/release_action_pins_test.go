package kubescape_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// ksailActionsRepository is the repository whose composite actions CD pins by commit.
const ksailActionsRepository = "devantler-tech/ksail"

// requirePinnedCommitsEnv makes a pinned commit missing from the local object store a failure. The
// release-graph-audit CI job fetches every pinned commit and sets it; elsewhere, a single-commit
// checkout skips the content audit visibly, because tests never read the network.
const requirePinnedCommitsEnv = "KSAIL_REQUIRE_PINNED_ACTION_COMMITS"

var (
	errActionAbsent       = errors.New("audited action is absent from CD")
	errActionPinsDisagree = errors.New("CD steps pin the audited action at different commits")
	errActionPinNotCommit = errors.New("audited action is not pinned to a full commit SHA")
	errPinnedContent      = errors.New("pinned action content does not match its audited witness")
	errPinnedRead         = errors.New("cannot read the action file at its pinned commit")
	errPinnedAbsent       = errors.New("the pinned commit is not in the local object store")
	errSiblingManifest    = errors.New("the pinned action has a sibling manifest")
)

// auditedActionBindings returns each audited file paired with the KSail action whose pinned commit runs it and
// the SHA-256 witness of its reviewed content.
func auditedActionBindings() []struct{ path, action, content string } {
	return []struct{ path, action, content string }{
		{
			".github/actions/release-admission/action.yaml",
			"release-admission",
			"a8ac217c7e2195fee64b364748a6724b99aa0aa4a9988127084f1d6b45953f0c",
		},
		{
			".github/actions/setup-desktop-build/action.yml",
			"setup-desktop-build",
			"473bc1c1f3e940ba58a76320cf8b6ff5e798bf5e9cbe68830469640ef1ef7cac",
		},
		{
			".github/actions/free-disk-space/action.yaml",
			"free-disk-space",
			"bb40116a5901c77432b0d330b05e56228d9546dab26f98a31f05a0e87ffab96c",
		},
		{
			".github/actions/free-disk-space/free-disk-space.sh",
			"free-disk-space",
			"2dd12fcf3779137ca1cb5f21194947f6a10d3f94f8fb438a1a29ec9021fbdc30",
		},
		{
			".github/actions/restore-ksail-binary/action.yaml",
			"restore-ksail-binary",
			"f1c049de4094024f62b97f5f70c98ababf6ed639ee59a16065c9814cffa61176",
		},
		// setup-desktop-build runs these scripts from its own pinned checkout ($GITHUB_ACTION_PATH).
		{
			".github/actions/free-disk-space/free-disk-space.sh",
			"setup-desktop-build",
			"2dd12fcf3779137ca1cb5f21194947f6a10d3f94f8fb438a1a29ec9021fbdc30",
		},
		{
			".github/scripts/download-go-modules.sh",
			"setup-desktop-build",
			"e5af8edf61a77de4397e952a05b1cc6ca0f83cd848523c4cf6b3aaa2539561fa",
		},
		{
			"scripts/stage-webui.sh",
			"setup-desktop-build",
			"5b3d7b0fa8b237f77ee9a88e6807e070c1b35c97df3d5f17357b3ecb8b2938a6",
		},
	}
}

// fullCommitSHA matches a full, lower-case Git commit SHA.
var fullCommitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// TestReleaseActionPinsMatchAuditedContent binds audited snapshots to the commits CI and CD
// execute for KSail's own composite actions. Each witness is the SHA-256 of an action file's
// audited content. The check reads that file at the commit the workflows actually pin and compares
// it with the witness, and the working tree must carry the same content, so a routine pin bump to a commit
// with the same content passes on its own, while a pin to changed or unaudited content fails.
// Changing an audited action requires a new reviewed content witness.
func TestReleaseActionPinsMatchAuditedContent(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	uses := releaseWorkflowUses(t, root)

	for _, binding := range auditedActionBindings() {
		t.Run(binding.action+":"+binding.path, func(t *testing.T) {
			t.Parallel()

			if workingTreeDigest(t, root, binding.path) != binding.content {
				t.Errorf(
					"audited content witness no longer matches the working tree's %s",
					binding.path,
				)
			}

			err := auditPinnedAction(
				t.Context(),
				root,
				uses,
				binding.action,
				binding.path,
				binding.content,
			)
			if errors.Is(err, errPinnedAbsent) && os.Getenv(requirePinnedCommitsEnv) != "true" {
				t.Skipf("%v; the release-graph-audit CI job fetches it and sets %s=true",
					err, requirePinnedCommitsEnv)
			}

			if err != nil {
				t.Error(err)
			}
		})
	}
}

// TestAuditPinnedActionRejectsUnauditedPins is the negative control for the pinned-content audit:
// a pin to a commit whose action content changed fails, as do disagreeing and abbreviated pins, a
// pin to a tree object, and a pin whose commit is not in the local object store.
func TestAuditPinnedActionRejectsUnauditedPins(t *testing.T) {
	t.Parallel()

	const path = ".github/actions/setup-desktop-build/action.yml"

	repo := t.TempDir()
	audited := []byte("name: Setup desktop build\n")
	auditedCommit := commitFixtureFile(t, repo, path, audited)
	changedCommit := commitFixtureFile(
		t,
		repo,
		path,
		[]byte("name: Setup desktop build\nchanged: true\n"),
	)

	tree := fixtureTree(t, repo, auditedCommit)
	witness := fmt.Sprintf("%x", sha256.Sum256(audited))
	reference := func(commit string) string {
		return ksailActionsRepository + "/.github/actions/setup-desktop-build@" + commit
	}

	err := auditPinnedAction(
		t.Context(), repo, []string{reference(auditedCommit), reference(auditedCommit)},
		"setup-desktop-build", path, witness,
	)
	if err != nil {
		t.Fatalf("a pin to the audited content must pass: %v", err)
	}

	for _, testCase := range []struct {
		name string
		uses []string
		want error
	}{
		{name: "changed content at a new pin", uses: []string{reference(changedCommit)}, want: errPinnedContent},
		{
			name: "consumers disagree",
			uses: []string{reference(auditedCommit), reference(changedCommit)},
			want: errActionPinsDisagree,
		},
		{name: "abbreviated pin", uses: []string{reference(auditedCommit[:12])}, want: errActionPinNotCommit},
		{name: "absent", uses: []string{"actions/checkout@" + auditedCommit}, want: errActionAbsent},
		{name: "tree object, not a commit", uses: []string{reference(tree)}, want: errActionPinNotCommit},
		{name: "commit not held locally", uses: []string{reference(strings.Repeat("a", 40))}, want: errPinnedAbsent},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := auditPinnedAction(
				t.Context(),
				repo,
				testCase.uses,
				"setup-desktop-build",
				path,
				witness,
			)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("want %v, got %v", testCase.want, err)
			}
		})
	}
}

// auditPinnedAction checks that every CD step using KSail's action agrees on one full commit SHA,
// and that the action file at that commit has the audited content.
func auditPinnedAction(
	ctx context.Context, root string, uses []string, action, path, witness string,
) error {
	pin, err := resolveActionPin(uses, action)
	if err != nil {
		return err
	}

	err = requireCommitObject(ctx, root, pin)
	if err != nil {
		return fmt.Errorf("%s@%s: %w", action, pin, err)
	}

	sibling, found := siblingManifest(path)
	if found && pinnedFileExists(ctx, root, pin, sibling) {
		return fmt.Errorf(
			"%w: %s@%s has %s besides %s", errSiblingManifest, action, pin, sibling, path,
		)
	}

	data, err := readPinnedFile(ctx, root, pin, path)
	if err != nil {
		return err
	}

	// A CRLF checkout (Windows, core.autocrlf) must not change the digest of unchanged content.
	digest := fmt.Sprintf("%x", sha256.Sum256(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))))
	if digest != witness {
		return fmt.Errorf(
			"%w: %s@%s has %s, the audited witness is %s",
			errPinnedContent, action, pin, digest, witness,
		)
	}

	return nil
}

// resolveActionPin returns the one full commit SHA every step in uses pins action at.
func resolveActionPin(uses []string, action string) (string, error) {
	prefix := ksailActionsRepository + "/.github/actions/" + action + "@"

	var pins []string

	for _, reference := range uses {
		pin, found := strings.CutPrefix(reference, prefix)
		if found && !slices.Contains(pins, pin) {
			pins = append(pins, pin)
		}
	}

	switch {
	case len(pins) == 0:
		return "", fmt.Errorf("%w: %s", errActionAbsent, action)
	case len(pins) > 1:
		return "", fmt.Errorf(
			"%w: %s at %s",
			errActionPinsDisagree,
			action,
			strings.Join(pins, ", "),
		)
	case !fullCommitSHA.MatchString(pins[0]):
		return "", fmt.Errorf("%w: %s@%s", errActionPinNotCommit, action, pins[0])
	}

	return pins[0], nil
}

// requireCommitObject checks that sha names a commit in the local object store. A tree or blob ID
// also matches fullCommitSHA and would serve `cat-file blob <sha>:<path>`, but workflows can only
// pin a commit. A missing object is errPinnedAbsent, never a pass.
func requireCommitObject(ctx context.Context, root, sha string) error {
	//nolint:gosec // G204: git with a SHA validated as 40 hex digits.
	kind, err := exec.CommandContext(ctx, "git", "-C", root, "cat-file", "-t", sha).Output()
	if err != nil {
		return fmt.Errorf("%w: %w", errPinnedAbsent, err)
	}

	if objectType := strings.TrimSpace(string(kind)); objectType != "commit" {
		return fmt.Errorf("%w: the object is a %s", errActionPinNotCommit, objectType)
	}

	return nil
}

// readPinnedFile returns path as committed at commit, from the local Git object store only.
func readPinnedFile(ctx context.Context, root, commit, path string) ([]byte, error) {
	//nolint:gosec // G204: git with a commit SHA validated as 40 hex digits and a fixed audited path.
	data, err := exec.CommandContext(
		ctx, "git", "-C", root, "cat-file", "blob", commit+":"+path,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s at %s: %w", errPinnedRead, path, commit, err)
	}

	return data, nil
}

// releaseWorkflowUses returns every step's `uses:` value in CI and CD.
func releaseWorkflowUses(t *testing.T, root string) []string {
	t.Helper()

	var (
		workflow struct {
			Jobs map[string]struct {
				Steps []struct {
					Uses string `yaml:"uses"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		uses []string
	)

	for _, name := range []string{"ci.yaml", "cd.yaml"} {
		workflow.Jobs = nil
		readYAML(t, filepath.Join(root, ".github", "workflows", name), &workflow)

		for _, job := range workflow.Jobs {
			for _, step := range job.Steps {
				if step.Uses != "" {
					uses = append(uses, step.Uses)
				}
			}
		}
	}

	return uses
}

// commitFixtureFile writes contents to path in the fixture repository at repo, commits it, and
// returns the new commit's SHA.
func commitFixtureFile(t *testing.T, repo, path string, contents []byte) string {
	t.Helper()

	git := func(args ...string) string {
		t.Helper()

		//nolint:gosec // G204: git on this test's own temporary fixture repository.
		command := exec.CommandContext(t.Context(), "git", append([]string{
			"-C", repo,
			"-c", "user.name=CI Fixture", "-c", "user.email=ci@example.invalid",
			"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null",
		}, args...)...)

		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}

		return strings.TrimSpace(string(output))
	}

	_, statErr := os.Stat(filepath.Join(repo, ".git"))
	if errors.Is(statErr, os.ErrNotExist) {
		git("init", "--quiet", "--object-format=sha1")
	}

	file := filepath.Join(repo, filepath.FromSlash(path))

	err := os.MkdirAll(filepath.Dir(file), 0o750)
	if err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(file), err)
	}

	err = os.WriteFile(file, contents, 0o600)
	if err != nil {
		t.Fatalf("write %s: %v", file, err)
	}

	git("add", path)
	git("commit", "--quiet", "-m", "test: fixture "+path)

	return git("rev-parse", "HEAD")
}

// workingTreeDigest returns the SHA-256 of the checked-out file at path, ignoring CRLF line endings.
func workingTreeDigest(t *testing.T, root, path string) string {
	t.Helper()

	file := filepath.Join(root, filepath.FromSlash(path))

	data, err := os.ReadFile(file) //nolint:gosec // G304: fixed audited paths.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return fmt.Sprintf("%x", sha256.Sum256(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))))
}

// fixtureTree returns the tree object ID of commit in the fixture repository at repo.
func fixtureTree(t *testing.T, repo, commit string) string {
	t.Helper()

	//nolint:gosec // G204: git on this test's own temporary fixture repository.
	output, err := exec.CommandContext(
		t.Context(), "git", "-C", repo, "rev-parse", commit+"^{tree}",
	).Output()
	if err != nil {
		t.Fatalf("resolve the fixture tree of %s: %v", commit, err)
	}

	return strings.TrimSpace(string(output))
}

// siblingManifest returns the other action manifest name next to an action.yaml or action.yml. When
// both exist, the runner loads action.yml, so auditing only one of them would miss the other.
func siblingManifest(path string) (string, bool) {
	if dir, found := strings.CutSuffix(path, "/action.yaml"); found {
		return dir + "/action.yml", true
	}

	if dir, found := strings.CutSuffix(path, "/action.yml"); found {
		return dir + "/action.yaml", true
	}

	return "", false
}

// pinnedFileExists reports whether path exists at commit. The commit is already known to be in the
// local object store, so a failed lookup means the file is absent.
func pinnedFileExists(ctx context.Context, root, commit, path string) bool {
	//nolint:gosec // G204: git with a commit SHA validated as 40 hex digits and a fixed audited path.
	return exec.CommandContext(ctx, "git", "-C", root, "cat-file", "-e", commit+":"+path).
		Run() ==
		nil
}

// TestAuditPinnedActionRejectsSiblingManifest is the negative control for a pinned commit that
// carries both action.yml and action.yaml: the audited manifest matches, but the runner could load
// the other one.
func TestAuditPinnedActionRejectsSiblingManifest(t *testing.T) {
	t.Parallel()

	const path = ".github/actions/setup-desktop-build/action.yml"

	repo := t.TempDir()
	audited := []byte("name: Setup desktop build\n")
	commitFixtureFile(t, repo, path, audited)
	shadowed := commitFixtureFile(
		t, repo, ".github/actions/setup-desktop-build/action.yaml", []byte("name: Unaudited\n"),
	)
	reference := ksailActionsRepository + "/.github/actions/setup-desktop-build@" + shadowed

	err := auditPinnedAction(
		t.Context(), repo, []string{reference}, "setup-desktop-build", path,
		fmt.Sprintf("%x", sha256.Sum256(audited)),
	)
	if !errors.Is(err, errSiblingManifest) {
		t.Fatalf("want %v, got %v", errSiblingManifest, err)
	}
}
