package gonotices

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	operationStage         = "stage"
	operationStageCLI      = "stage-cli"
	operationVerify        = "verify"
	operationVerifyCLI     = "verify-cli"
	releaseGroupArguments  = 3
	releaseTargetArguments = 4
	sourceCheckTimeout     = 10 * time.Second
	maxSourceCommandOutput = 4096
	keyedToolchainValues   = 2
)

type releaseCommand struct {
	operation, version, source, directory string
	targets                               []string
}

// Run executes only closed release staging and verification operations. It never
// loads dependency graphs, invokes compilers for release targets, or fetches data.
func Run(root string, arguments []string, output io.Writer) error {
	plan, err := planReleaseCommand(arguments)
	if err != nil {
		return err
	}

	err = verifySourceCheckout(root, plan.source)
	if err != nil {
		return err
	}

	actual, err := runningToolchain(root)
	if err != nil {
		return err
	}

	for _, target := range plan.targets {
		release := Release{plan.version, plan.source, target}

		err = runReleaseTarget(root, plan, release, actual)
		if err != nil {
			return err
		}

		_, err = fmt.Fprintf(output, "Verified Go recipient material for %s\n", target)
		if err != nil {
			return ErrIncomplete
		}
	}

	return nil
}

func planReleaseCommand(arguments []string) (releaseCommand, error) {
	if len(arguments) == 0 {
		return releaseCommand{}, ErrIncomplete
	}

	var (
		plan releaseCommand
		err  error
	)

	switch arguments[0] {
	case operationStageCLI, operationVerifyCLI:
		plan, err = planGroupCommand(arguments)
	case operationStage, operationVerify:
		plan, err = planTargetCommand(arguments)
	default:
		return plan, ErrIncomplete
	}

	if err != nil {
		return plan, err
	}

	for _, target := range plan.targets {
		if !validRelease(Release{plan.version, plan.source, target}) {
			return plan, ErrIncomplete
		}
	}

	return plan, nil
}

func planGroupCommand(arguments []string) (releaseCommand, error) {
	plan := releaseCommand{operation: arguments[0], directory: "dist"}
	if len(arguments) != releaseGroupArguments {
		return plan, ErrIncomplete
	}

	plan.version, plan.source = arguments[1], arguments[2]

	plan.targets = recipientTargets()[:5]

	return plan, nil
}

func planTargetCommand(arguments []string) (releaseCommand, error) {
	plan := releaseCommand{operation: arguments[0], directory: "."}
	if len(arguments) != releaseTargetArguments {
		return plan, ErrIncomplete
	}

	target := arguments[1]
	if plan.operation == operationVerify && !strings.HasPrefix(target, "desktop-") {
		return plan, ErrIncomplete
	}

	plan.targets = []string{target}

	plan.version, plan.source = arguments[2], arguments[3]
	if target == "desktop-darwin-arm64" {
		plan.directory = "dist-desktop"
	}

	return plan, nil
}

func runReleaseTarget(root string, plan releaseCommand, release Release, actual Toolchain) error {
	if plan.operation == operationStage || plan.operation == operationStageCLI {
		_, err := Stage(root, release, actual)

		return err
	}

	payload, err := Prepare(root, release, actual)
	if err != nil {
		return err
	}

	layout, err := ReleaseLayout(release)
	if err != nil {
		return err
	}

	if !ordinaryDirectories(root, plan.directory) {
		return ErrIncomplete
	}

	return VerifyArchive(filepath.Join(root, plan.directory, layout.Archive), release, payload)
}

func runningToolchain(root string) (Toolchain, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sourceCheckTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, "go", "env", "GOROOT", "GOVERSION")

	output, err := runSourceCommand(root, command)
	if err != nil {
		return Toolchain{}, err
	}

	values := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(values) != keyedToolchainValues || !filepath.IsAbs(values[0]) {
		return Toolchain{}, ErrIncomplete
	}

	directory, err := linkFreeToolchainDirectory(values[0])
	if err != nil {
		return Toolchain{}, err
	}

	selected := Toolchain{
		Directory:   directory,
		Version:     values[1],
		LicenseFile: filepath.Join(directory, primaryLicenseFilename),
	}

	info, inspectErr := os.Lstat(selected.LicenseFile)
	if inspectErr != nil || !info.Mode().IsRegular() {
		selected.LicenseFile = filepath.Join(
			filepath.Dir(selected.Directory),
			primaryLicenseFilename,
		)
	}

	return selected, nil
}

// maxToolchainLinks bounds how many links GOROOT itself may pass through.
const maxToolchainLinks = 8

// linkFreeToolchainDirectory resolves a GOROOT that is itself a link to the
// directory it names. actions/setup-go on Windows installs Go on D: and links
// C:\hostedtoolcache\...\x64 to it with a junction, and `go env GOROOT` reports
// the linked path. Only the root is resolved: every read beneath it still
// refuses links.
func linkFreeToolchainDirectory(directory string) (string, error) {
	for range maxToolchainLinks {
		info, err := os.Lstat(directory)
		if err != nil {
			return "", ErrIncomplete
		}

		if info.IsDir() {
			return directory, nil
		}

		target, err := os.Readlink(directory)
		if err != nil {
			return "", ErrIncomplete
		}

		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(directory), target)
		}

		directory = filepath.Clean(target)
	}

	return "", ErrIncomplete
}

func verifySourceCheckout(root, source string) error {
	ctx, cancel := context.WithTimeout(context.Background(), sourceCheckTimeout)
	defer cancel()

	top, err := runSourceCommand(
		root,
		exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel"),
	)
	if err != nil || !sameDirectory(root, strings.TrimSpace(string(top))) {
		return ErrIncomplete
	}

	err = verifySourceHead(ctx, root, source)
	if err != nil {
		return err
	}

	_, err = runSourceCommand(root, sourceFilesCommand(ctx))
	if err != nil {
		return err
	}

	_, err = runSourceCommand(
		root,
		exec.CommandContext(
			ctx,
			"git",
			"-c",
			"core.fsmonitor=false",
			"diff",
			"--quiet",
			"--no-ext-diff",
			"--no-textconv",
			"HEAD",
			"--",
			"go.mod",
			"go.sum",
			".github/go-license-policy.json",
			".github/go-notice-materials.json",
			".github/go-notice-receipts.json",
			".goreleaser.yaml",
			".goreleaser.desktop.yaml",
			".github/workflows/cd.yaml",
			"notices/go",
			"third_party",
		),
	)
	if err != nil {
		return err
	}

	return verifySourceHead(ctx, root, source)
}

func verifySourceHead(ctx context.Context, root, source string) error {
	head, err := runSourceCommand(
		root,
		exec.CommandContext(ctx, "git", "rev-parse", "--verify", "HEAD"),
	)
	if err != nil || strings.TrimSpace(string(head)) != source {
		return ErrIncomplete
	}

	return nil
}

func sourceFilesCommand(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(
		ctx,
		"git",
		"-c",
		"core.fsmonitor=false",
		"ls-files",
		"--error-unmatch",
		"--",
		"go.mod",
		"go.sum",
		".github/go-license-policy.json",
		".github/go-notice-materials.json",
		".github/go-notice-receipts.json",
		".goreleaser.yaml",
		".goreleaser.desktop.yaml",
		".github/workflows/cd.yaml",
		"notices/go/cli-darwin-arm64/THIRD_PARTY_NOTICES.txt",
		"notices/go/cli-darwin-arm64/manifest.json",
		"notices/go/cli-linux-amd64/THIRD_PARTY_NOTICES.txt",
		"notices/go/cli-linux-amd64/manifest.json",
		"notices/go/cli-linux-arm64/THIRD_PARTY_NOTICES.txt",
		"notices/go/cli-linux-arm64/manifest.json",
		"notices/go/cli-windows-amd64/THIRD_PARTY_NOTICES.txt",
		"notices/go/cli-windows-amd64/manifest.json",
		"notices/go/cli-windows-arm64/THIRD_PARTY_NOTICES.txt",
		"notices/go/cli-windows-arm64/manifest.json",
		"notices/go/desktop-darwin-arm64/THIRD_PARTY_NOTICES.txt",
		"notices/go/desktop-darwin-arm64/manifest.json",
		"notices/go/desktop-linux-amd64/THIRD_PARTY_NOTICES.txt",
		"notices/go/desktop-linux-amd64/manifest.json",
		"notices/go/desktop-windows-amd64/THIRD_PARTY_NOTICES.txt",
		"notices/go/desktop-windows-amd64/manifest.json",
	)
}

func sameDirectory(first, second string) bool {
	left, leftErr := os.Stat(first)
	right, rightErr := os.Stat(second)

	return leftErr == nil && rightErr == nil && left.IsDir() && right.IsDir() &&
		os.SameFile(left, right)
}

type sourceCommandOutput struct{ data bytes.Buffer }

func (output *sourceCommandOutput) Write(data []byte) (int, error) {
	if len(data) > maxSourceCommandOutput-output.data.Len() {
		return 0, ErrIncomplete
	}

	written, err := output.data.Write(data)
	if err != nil {
		return written, ErrIncomplete
	}

	return written, nil
}

func runSourceCommand(root string, command *exec.Cmd) ([]byte, error) {
	command.Dir = root

	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") && !strings.HasPrefix(entry, "GOPROXY=") {
			command.Env = append(command.Env, entry)
		}
	}

	command.Env = append(
		command.Env,
		"GOPROXY=off",
		"GIT_NO_LAZY_FETCH=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
	)

	var output sourceCommandOutput

	command.Stdout = &output
	command.Stderr = io.Discard

	err := command.Run()
	if err != nil {
		return nil, ErrIncomplete
	}

	return output.data.Bytes(), nil
}
