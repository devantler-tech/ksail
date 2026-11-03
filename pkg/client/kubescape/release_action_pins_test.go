package kubescape_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// ksailActionsRepository is the repository whose composite actions CD pins by commit.
const ksailActionsRepository = "devantler-tech/ksail"

// pinnedContentAttempts and pinnedContentTimeout bound reading an action file at a pinned commit
// from GitHub when the local clone does not hold that commit (CI checks out one commit only).
const (
	pinnedContentAttempts = 3
	pinnedContentTimeout  = 30 * time.Second
)

var (
	errActionAbsent       = errors.New("audited action is absent from CD")
	errActionPinsDisagree = errors.New("CD steps pin the audited action at different commits")
	errActionPinNotCommit = errors.New("audited action is not pinned to a full commit SHA")
	errPinnedContent      = errors.New("pinned action content does not match its audited witness")
	errPinnedRead         = errors.New("cannot read the action file at its pinned commit")
)

// fullCommitSHA matches a full, lower-case Git commit SHA.
var fullCommitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// TestReleaseActionPinsMatchAuditedContent binds audited snapshots to the commits CD executes.
// Each witness is the SHA-256 of an action file's audited content. The check reads that file at
// the commit CD actually pins and compares it with the witness, so a routine pin bump to a commit
// with the same content passes on its own, while a pin to changed or unaudited content fails.
// Changing an audited action requires a new reviewed content witness.
func TestReleaseActionPinsMatchAuditedContent(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	actual := releaseConfigDigests(t, root)
	uses := releaseWorkflowUses(t, root)

	for _, binding := range []struct{ path, action, content string }{
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
	} {
		t.Run(binding.path, func(t *testing.T) {
			t.Parallel()

			if actual[binding.path] != binding.content {
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
			if err != nil {
				t.Error(err)
			}
		})
	}
}

// TestAuditPinnedActionRejectsUnauditedPins is the negative control for the pinned-content audit:
// a pin to a commit whose action content changed fails, as do disagreeing and abbreviated pins.
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
		return fmt.Errorf("%w: %s", errActionAbsent, action)
	case len(pins) > 1:
		return fmt.Errorf("%w: %s at %s", errActionPinsDisagree, action, strings.Join(pins, ", "))
	case !fullCommitSHA.MatchString(pins[0]):
		return fmt.Errorf("%w: %s@%s", errActionPinNotCommit, action, pins[0])
	}

	data, err := readPinnedFile(ctx, root, pins[0], path)
	if err != nil {
		return err
	}

	// A CRLF checkout (Windows, core.autocrlf) must not change the digest of unchanged content.
	digest := fmt.Sprintf("%x", sha256.Sum256(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))))
	if digest != witness {
		return fmt.Errorf(
			"%w: %s@%s has %s, the audited witness is %s",
			errPinnedContent, action, pins[0], digest, witness,
		)
	}

	return nil
}

// readPinnedFile returns path as committed at commit. It reads the local Git object store first
// and falls back to GitHub's raw file endpoint, because CI checks out only the commit under test.
func readPinnedFile(ctx context.Context, root, commit, path string) ([]byte, error) {
	//nolint:gosec // G204: git with a commit SHA validated as 40 hex digits and a fixed audited path.
	data, gitErr := exec.CommandContext(
		ctx, "git", "-C", root, "cat-file", "blob", commit+":"+path,
	).Output()
	if gitErr == nil {
		return data, nil
	}

	url := fmt.Sprintf(
		"https://raw.githubusercontent.com/%s/%s/%s",
		ksailActionsRepository,
		commit,
		path,
	)

	var errs []error

	for attempt := range pinnedContentAttempts {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		data, err := fetchPinnedFile(ctx, url)
		if err == nil {
			return data, nil
		}

		errs = append(errs, err)
	}

	return nil, fmt.Errorf(
		"%w: %s at %s (local Git: %w; GitHub: %w)",
		errPinnedRead, path, commit, gitErr, errors.Join(errs...),
	)
}

func fetchPinnedFile(ctx context.Context, url string) ([]byte, error) {
	requestCtx, cancel := context.WithTimeout(ctx, pinnedContentTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", url, err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get %s: %w: %s", url, errPinnedRead, response.Status)
	}

	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}

	return data, nil
}

// releaseWorkflowUses returns every step's `uses:` value in CD.
func releaseWorkflowUses(t *testing.T, root string) []string {
	t.Helper()

	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Uses string `yaml:"uses"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	readYAML(t, filepath.Join(root, ".github", "workflows", "cd.yaml"), &workflow)

	var uses []string

	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if step.Uses != "" {
				uses = append(uses, step.Uses)
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
		git("init", "--quiet")
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
