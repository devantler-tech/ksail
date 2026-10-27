//go:build ignore

// key.go is run explicitly by the composite action. Its behavioral tests execute
// that action step against real, dependency-free Go modules in internal/ciharness.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const discoveryTimeout = 2 * time.Minute

var (
	errMissingInputs    = errors.New("toolchain, bundled CLI, action path and target are required")
	errCustomFlags      = errors.New("custom GOFLAGS require a separate reviewed cache recipe")
	errToolchain        = errors.New("setup-go version does not match the active toolchain")
	errWorkspace        = errors.New("workspace builds require a separate reviewed cache recipe")
	errLocalReplacement = errors.New("a local replacement escapes the checkout")
	errModuleMetadata   = errors.New("local module metadata escapes the checkout")
	errMainMissing      = errors.New("no main executable was resolved")
)

type module struct {
	Version string  `json:"version"`
	GoMod   string  `json:"goMod"`
	Replace *module `json:"replace"`
}

// These are go list's build inputs, excluding TestGoFiles/TestEmbedFiles.
// EmbedFiles still includes an explicitly embedded *_test.go file.
type goPackage struct {
	Dir          string   `json:"dir"`
	Name         string   `json:"name"`
	GoFiles      []string `json:"goFiles"`
	CgoFiles     []string `json:"cgoFiles"`
	CFiles       []string `json:"cFiles"`
	CXXFiles     []string `json:"cxxFiles"`
	MFiles       []string `json:"mFiles"`
	HFiles       []string `json:"hFiles"`
	FFiles       []string `json:"fFiles"`
	SFiles       []string `json:"sFiles"`
	SwigFiles    []string `json:"swigFiles"`
	SwigCXXFiles []string `json:"swigCxxFiles"`
	SysoFiles    []string `json:"sysoFiles"`
	EmbedFiles   []string `json:"embedFiles"`
	Module       *module  `json:"module"`
}

type inputs struct {
	Environment map[string]string `json:"environment"`
	Files       map[string]string `json:"files"`
}

func main() {
	err := run()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "binary cache inputs:", err)

		os.Exit(1)
	}
}

func run() error {
	goVersion := flag.String("go-version", "", "setup-go version")
	cliVersion := flag.String("cli-version", "", "bundled Copilot CLI version")
	actionPath := flag.String("action-path", "", "composite action directory")
	goos := flag.String("goos", "", "binary target operating system")
	goarch := flag.String("goarch", "", "binary target architecture")

	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), discoveryTimeout)
	defer cancel()

	key, err := calculate(ctx, *goVersion, *cliVersion, *actionPath, *goos, *goarch)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(os.Stdout, key)
	if err != nil {
		return fmt.Errorf("write binary cache key: %w", err)
	}

	return nil
}

func calculate(
	ctx context.Context,
	version, cliVersion, actionPath, goos, goarch string,
) (string, error) {
	err := validateInputs(version, cliVersion, actionPath, goos, goarch)
	if err != nil {
		return "", err
	}

	root, directory, err := checkoutRoot()
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()

	environment := append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch)

	settings, err := readEnvironment(ctx, version, environment)
	if err != nil {
		return "", err
	}

	data := inputs{Environment: settings, Files: map[string]string{}}

	data.Environment["copilot-cli-version"] = cliVersion
	for _, name := range []string{"go.mod", "go.sum"} {
		err = addFile(root, name, data.Files, name == "go.sum")
		if err != nil {
			return "", err
		}
	}

	err = collectPackages(ctx, directory, root, environment, data.Files)
	if err != nil {
		return "", err
	}

	err = collectRecipe(actionPath, data.Files)
	if err != nil {
		return "", err
	}

	encoded, err := json.Marshal(data) // JSON sorts map keys; no checkout paths enter the digest.
	if err != nil {
		return "", fmt.Errorf("encode build inputs: %w", err)
	}

	return fmt.Sprintf("ksail-bin-v2-%x", sha256.Sum256(encoded)), nil
}

func validateInputs(version, cliVersion, actionPath, goos, goarch string) error {
	if version == "" || cliVersion == "" || actionPath == "" || goos == "" || goarch == "" {
		return errMissingInputs
	}

	return nil
}

func checkoutRoot() (*os.Root, string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("resolve checkout: %w", err)
	}

	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, "", fmt.Errorf("resolve checkout links: %w", err)
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, "", fmt.Errorf("open checkout: %w", err)
	}

	return root, directory, nil
}

func readEnvironment(
	ctx context.Context,
	version string,
	environment []string,
) (map[string]string, error) {
	command := exec.CommandContext(ctx, "go", "env", "-json",
		"GOVERSION", "GOOS", "GOARCH", "CGO_ENABLED", "GOAMD64", "GOARM64", "GOARM", "GO386",
		"GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64", "GOEXPERIMENT", "GOFLAGS", "CC", "CXX",
		"CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_FFLAGS", "CGO_LDFLAGS", "GOWORK")
	command.Env = environment

	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("read effective Go environment: %w", err)
	}

	var settings map[string]string

	err = json.Unmarshal(output, &settings)
	if err != nil {
		return nil, fmt.Errorf("decode Go environment: %w", err)
	}

	err = validateEnvironment(version, settings)
	if err != nil {
		return nil, err
	}

	delete(settings, "GOWORK")

	for _, name := range []string{"RUNNER_OS", "RUNNER_ARCH", "ImageOS", "ImageVersion"} {
		settings[name] = os.Getenv(name)
	}

	return settings, nil
}

func validateEnvironment(version string, settings map[string]string) error {
	for option := range strings.FieldsSeq(settings["GOFLAGS"]) {
		if option != "-mod=readonly" && option != "-mod=vendor" &&
			(!strings.HasPrefix(option, "-tags=") || len(option) <= len("-tags=")) {
			return errCustomFlags
		}
	}

	if strings.TrimPrefix(settings["GOVERSION"], "go") != version {
		return errToolchain
	}

	if workspace := settings["GOWORK"]; workspace != "" && workspace != "off" {
		return errWorkspace
	}

	return nil
}

func collectRecipe(actionPath string, files map[string]string) error {
	action, err := os.OpenRoot(actionPath)
	if err != nil {
		return fmt.Errorf("open cache recipe: %w", err)
	}
	defer func() { _ = action.Close() }()

	for _, name := range []string{"action.yaml", "key.go"} {
		contents, readErr := action.ReadFile(name)
		if readErr != nil {
			return fmt.Errorf("read cache recipe %s: %w", name, readErr)
		}

		files["@recipe/"+name] = fmt.Sprintf("%x", sha256.Sum256(contents))
	}

	return nil
}

func collectPackages(
	ctx context.Context,
	directory string,
	root *os.Root,
	environment []string,
	files map[string]string,
) error {
	command := exec.CommandContext(ctx, "go", "list", "-deps", "-json", ".")
	command.Env = environment

	var diagnostic bytes.Buffer

	command.Stderr = &diagnostic

	pipe, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open Go input stream: %w", err)
	}

	err = command.Start()
	if err != nil {
		return fmt.Errorf("start Go input discovery: %w", err)
	}

	mainFound, failure := readPackages(json.NewDecoder(pipe), directory, root, files)
	if failure != nil {
		_ = command.Process.Kill()
	}

	waitErr := command.Wait()

	if failure != nil {
		return failure
	}

	if waitErr != nil {
		return fmt.Errorf("resolve effective Go inputs: %w: %s", waitErr, diagnostic.String())
	}

	if !mainFound {
		return errMainMissing
	}

	return nil
}

func readPackages(
	decoder *json.Decoder,
	directory string,
	root *os.Root,
	files map[string]string,
) (bool, error) {
	mainFound := false

	for {
		var pkg goPackage

		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			return mainFound, nil
		}

		if err != nil {
			return false, fmt.Errorf("decode Go package: %w", err)
		}

		isMain, err := collectPackage(pkg, directory, root, files)
		if err != nil {
			return false, err
		}

		mainFound = mainFound || isMain
	}
}

func collectPackage(
	pkg goPackage,
	directory string,
	root *os.Root,
	files map[string]string,
) (bool, error) {
	relative, err := filepath.Rel(directory, pkg.Dir)
	if err != nil {
		return false, fmt.Errorf("resolve package directory: %w", err)
	}

	if !filepath.IsLocal(relative) {
		if pkg.Module != nil && pkg.Module.Replace != nil && pkg.Module.Replace.Version == "" {
			return false, errLocalReplacement
		}
		// Registry dependencies are content-addressed by go.mod/go.sum;
		// standard-library inputs are bound to the active Go toolchain.
		return false, nil
	}

	err = collectSourceFiles(pkg, relative, root, files)
	if err != nil {
		return false, err
	}

	err = collectModule(pkg.Module, directory, root, files)
	if err != nil {
		return false, err
	}

	return relative == "." && pkg.Name == "main", nil
}

func collectSourceFiles(
	pkg goPackage,
	relative string,
	root *os.Root,
	files map[string]string,
) error {
	groups := [][]string{
		pkg.GoFiles,
		pkg.CgoFiles,
		pkg.CFiles,
		pkg.CXXFiles,
		pkg.MFiles,
		pkg.HFiles,
		pkg.FFiles,
		pkg.SFiles,
		pkg.SwigFiles,
		pkg.SwigCXXFiles,
		pkg.SysoFiles,
		pkg.EmbedFiles,
	}
	for _, group := range groups {
		for _, name := range group {
			err := addFile(root, filepath.Join(relative, name), files, false)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func collectModule(mod *module, directory string, root *os.Root, files map[string]string) error {
	if mod == nil || mod.GoMod == "" {
		return nil
	}

	name, err := filepath.Rel(directory, mod.GoMod)
	if err != nil || !filepath.IsLocal(name) {
		return errModuleMetadata
	}

	return addFile(root, name, files, false)
}

func addFile(root *os.Root, name string, files map[string]string, optional bool) error {
	contents, err := root.ReadFile(name)
	if optional && errors.Is(err, os.ErrNotExist) {
		files[filepath.ToSlash(name)] = "absent"

		return nil
	}

	if err != nil {
		return fmt.Errorf("read build input %s: %w", name, err)
	}

	files[filepath.ToSlash(name)] = fmt.Sprintf("%x", sha256.Sum256(contents))

	return nil
}
