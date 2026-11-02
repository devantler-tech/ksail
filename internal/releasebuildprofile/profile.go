// Package releasebuildprofile admits one fixed, non-publishing release experiment.
package releasebuildprofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

// Frozen cohort identities and bounds.
const (
	baselineArm        = "baseline"
	candidateArm       = "candidate"
	pairCount          = 3
	armCount           = 2
	outerParallelism   = 4
	SourceSHA          = "a3a3957d45f8071c3be2fd2fc96e699459d01d73"
	SourceTag          = "v7.206.65"
	ConfigBlob         = "91dd87214face749e2f061e59bd9ead039720c56"
	ConfigSHA256       = "5ac329a9712c6fc075096468790c855fff3474085e337c13f3969cf465789f7a"
	GoVersion          = "go1.27.3"
	ReleaserVersion    = "2.19.0"
	MaximumReportBytes = 64 * 1024
)

var (
	errInvalid = errors.New("release experiment is incomplete or incompatible")
	hex40      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	hex64      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	decimal    = regexp.MustCompile(`^[1-9][0-9]*$`)
)

// Output describes one independently inspected executable, never its location.
type Output struct {
	Target      string `json:"target"`
	SHA256      string `json:"sha256"`
	GoVersion   string `json:"goVersion"`
	Commit      string `json:"commit"`
	VCSModified *bool  `json:"vcsModified"`
}

// Limits distinguishes a verified unlimited constraint from an absent read.
type Limits struct {
	Verified    bool            `json:"verified"`
	MemoryBytes json.RawMessage `json:"memoryBytes"`
	SwapBytes   json.RawMessage `json:"swapBytes"`
}

// Environment binds the hardware, toolchain and effective baseline controls.
type Environment struct {
	RunnerImage         string  `json:"runnerImage"`
	RunnerVersion       string  `json:"runnerVersion"`
	ProcessorHash       string  `json:"processorHash"`
	ConstraintsHash     string  `json:"constraintsHash"`
	PhysicalMemoryBytes uint64  `json:"physicalMemoryBytes"`
	HostSwapBytes       *uint64 `json:"hostSwapBytes"`
	Kernel              string  `json:"kernel"`
	PackagesHash        string  `json:"packagesHash"`
	GoVersion           string  `json:"goVersion"`
	GoreleaserVersion   string  `json:"goreleaserVersion"`
	OuterParallelism    int     `json:"outerParallelism"`
	DefaultChildProcs   int     `json:"defaultChildProcs"`
	GoMemLimit          string  `json:"goMemLimit"`
	NodeVersion         string  `json:"nodeVersion"`
	NPMVersion          string  `json:"npmVersion"`
	Limits              Limits  `json:"limits"`
}

// Resources records aggregate job observations, with incomplete reads explicit.
type Resources struct {
	Scope                 string `json:"scope"`
	SampleIntervalSeconds int    `json:"sampleIntervalSeconds"`
	CPUUsageMicros        uint64 `json:"cpuUsageMicros"`
	Complete              bool   `json:"complete"`
	MemoryPeakBytes       uint64 `json:"memoryPeakBytes"`
	MinimumFreeDiskBytes  uint64 `json:"minimumFreeDiskBytes"`
	OOMKills              uint64 `json:"oomKills"`
	SwapOutPages          uint64 `json:"swapOutPages"`
	SampleCount           int    `json:"sampleCount"`
}

// Report is the bounded evidence from one declared pair member.
type Report struct {
	SchemaVersion      int         `json:"schemaVersion"`
	Complete           bool        `json:"complete"`
	Pair               int         `json:"pair"`
	Arm                string      `json:"arm"`
	SourceSHA          string      `json:"sourceSha"`
	Tag                string      `json:"tag"`
	ConfigBlob         string      `json:"configBlob"`
	HarnessSHA         string      `json:"harnessSha"`
	RunID              string      `json:"runId"`
	Attempt            string      `json:"attempt"`
	Environment        Environment `json:"environment"`
	ChildProcs         int         `json:"childProcs"`
	ElapsedSeconds     float64     `json:"elapsedSeconds"`
	BinaryBuildSeconds float64     `json:"binaryBuildSeconds"`
	Resources          Resources   `json:"resources"`
	Inputs             struct {
		UIHash      string `json:"uiHash"`
		BundlerHash string `json:"bundlerHash"`
	} `json:"inputs"`
	NativeCLI struct {
		Version    bool `json:"version"`
		Help       bool `json:"help"`
		SchemaFlag bool `json:"schemaFlag"`
	} `json:"nativeCli"`
	Outputs []Output `json:"outputs"`
}

// Expected binds the cohort to the collecting workflow, rather than its first file.
type Expected struct{ HarnessSHA, RunID, Attempt string }

// PairResult preserves every observation, including slower candidates.
type PairResult struct {
	Pair             int     `json:"pair"`
	BaselineSeconds  float64 `json:"baselineSeconds"`
	CandidateSeconds float64 `json:"candidateSeconds"`
	Improvement      float64 `json:"improvement"`
}

// Result qualifies a candidate for review; it never authorizes adoption.
type Result struct {
	Complete bool         `json:"complete"`
	Decision string       `json:"decision"`
	Pairs    []PairResult `json:"pairs"`
}

// PrepareConfig permits only the common date normalization and one treatment.
func PrepareConfig(original []byte, arm string) ([]byte, error) {
	if (arm != baselineArm && arm != candidateArm) || Digest(original) != ConfigSHA256 {
		return nil, errInvalid
	}

	date := []byte("buildmeta.Date={{.Date}}")
	if bytes.Count(original, date) != 1 ||
		bytes.Count(original, []byte("      - CGO_ENABLED=0\n")) != 1 {
		return nil, errInvalid
	}

	configured := bytes.Replace(original, date, []byte("buildmeta.Date={{.CommitDate}}"), 1)
	if arm == candidateArm {
		configured = bytes.Replace(configured, []byte("      - CGO_ENABLED=0\n"),
			[]byte("      - CGO_ENABLED=0\n      - GOMAXPROCS=1\n"), 1)
	}

	return configured, nil
}

// Digest provides strong content identity for compact inventories.
func Digest(value []byte) string {
	sum := sha256.Sum256(value)

	return hex.EncodeToString(sum[:])
}

// ReadReport rejects extra documents, unexpected fields and oversized evidence.
func ReadReport(input io.Reader) (Report, error) {
	var report Report

	data, err := io.ReadAll(io.LimitReader(input, MaximumReportBytes+1))
	if err != nil || len(data) > MaximumReportBytes {
		return report, errInvalid
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	err = decoder.Decode(&report)
	if err != nil {
		return report, errInvalid
	}

	err = decoder.Decode(new(any))
	if !errors.Is(err, io.EOF) {
		return report, errInvalid
	}

	return report, nil
}

func validLimit(raw json.RawMessage) bool {
	if bytes.Equal(raw, []byte("null")) {
		return true
	}

	var value uint64

	return len(raw) > 0 && json.Unmarshal(raw, &value) == nil
}

func validReport(report Report, expected Expected) bool {
	return validIdentity(report, expected) && validEnvironment(report.Environment) &&
		validMeasurement(report) && validResources(report.Resources) &&
		validOutputs(report.Outputs) && validInputs(report)
}

func validIdentity(report Report, expected Expected) bool {
	return validSource(report) && report.Pair >= 1 && report.Pair <= pairCount &&
		(report.Arm == baselineArm || report.Arm == candidateArm) &&
		report.HarnessSHA == expected.HarnessSHA && report.RunID == expected.RunID && report.Attempt == expected.Attempt
}

func validSource(report Report) bool {
	return report.SchemaVersion == 1 && report.Complete && report.SourceSHA == SourceSHA &&
		report.Tag == SourceTag && report.ConfigBlob == ConfigBlob
}

func validEnvironment(environment Environment) bool {
	return validHardware(environment) && validToolchain(environment) &&
		environment.Limits.Verified &&
		validLimit(environment.Limits.MemoryBytes) &&
		validLimit(environment.Limits.SwapBytes)
}

func validHardware(environment Environment) bool {
	return environment.RunnerImage != "" && environment.RunnerVersion != "" &&
		hex64.MatchString(
			environment.ProcessorHash,
		) && hex64.MatchString(environment.ConstraintsHash) &&
		hex64.MatchString(environment.PackagesHash) && environment.PhysicalMemoryBytes > 0 &&
		environment.Kernel != "" && environment.HostSwapBytes != nil
}

func validToolchain(environment Environment) bool {
	return environment.GoVersion == GoVersion && environment.GoreleaserVersion == ReleaserVersion &&
		environment.OuterParallelism == outerParallelism && environment.DefaultChildProcs >= 1 &&
		environment.GoMemLimit == "6GiB" && environment.NodeVersion != "" && environment.NPMVersion != ""
}

func validMeasurement(report Report) bool {
	const maximumSeconds = 55 * 60

	expectedChildProcs := report.Environment.DefaultChildProcs
	if report.Arm == candidateArm {
		expectedChildProcs = 1
	}

	return report.ChildProcs == expectedChildProcs && report.ElapsedSeconds > 0 &&
		report.ElapsedSeconds <= maximumSeconds && report.BinaryBuildSeconds > 0 &&
		report.BinaryBuildSeconds <= report.ElapsedSeconds+1
}

func validResources(resources Resources) bool {
	const intervalSeconds, minimumSamples, minimumDiskBytes = 5, 2, 1 << 30

	return resources.Complete && resources.Scope == "job-cgroup-peak;host-swap;sampled-disk" &&
		resources.SampleIntervalSeconds == intervalSeconds && resources.CPUUsageMicros > 0 &&
		resources.SampleCount >= minimumSamples && resources.MemoryPeakBytes > 0 &&
		resources.MinimumFreeDiskBytes >= minimumDiskBytes && resources.OOMKills == 0
}

func validInputs(report Report) bool {
	return hex64.MatchString(report.Inputs.UIHash) &&
		hex64.MatchString(report.Inputs.BundlerHash) &&
		report.NativeCLI.Version &&
		report.NativeCLI.Help &&
		report.NativeCLI.SchemaFlag
}

func releaseTargets() []string {
	return strings.Fields("darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64")
}

func validOutputs(outputs []Output) bool {
	if len(outputs) != len(releaseTargets()) {
		return false
	}

	seen := map[string]bool{}
	for _, output := range outputs {
		if seen[output.Target] || !validOutput(output) {
			return false
		}

		seen[output.Target] = true
	}

	for _, target := range releaseTargets() {
		if !seen[target] {
			return false
		}
	}

	return true
}

func validOutput(output Output) bool {
	return hex64.MatchString(output.SHA256) && output.GoVersion == GoVersion &&
		output.Commit == SourceSHA && output.VCSModified != nil && !*output.VCSModified
}

// CompareFiles requires all six declared reports and never replaces a sample.
func CompareFiles(directory string, expected Expected) (Result, error) {
	result := Result{Decision: "inconclusive"}
	if !hex40.MatchString(expected.HarnessSHA) || !decimal.MatchString(expected.RunID) ||
		!decimal.MatchString(expected.Attempt) {
		return result, errInvalid
	}

	reports, err := readReports(directory, expected)
	if err != nil {
		return result, err
	}

	qualified := true

	for pair := 1; pair <= pairCount; pair++ {
		baseline, candidate := reports[pair][baselineArm], reports[pair][candidateArm]
		if !compatiblePair(baseline, candidate) || !sameProgram(reports[1][baselineArm], baseline) {
			return result, errInvalid
		}

		qualified = qualified && qualifies(baseline, candidate)
		result.Pairs = append(result.Pairs, PairResult{
			Pair: pair, BaselineSeconds: baseline.BinaryBuildSeconds,
			CandidateSeconds: candidate.BinaryBuildSeconds,
			Improvement:      1 - candidate.BinaryBuildSeconds/baseline.BinaryBuildSeconds,
		})
	}

	result.Complete = true
	result.Decision = "retain-baseline"

	if qualified {
		result.Decision = "candidate-qualified"
	}

	return result, nil
}

func readReports(directory string, expected Expected) (map[int]map[string]Report, error) {
	canonical, err := fsutil.EvalCanonicalPath(directory)
	if err != nil {
		return nil, errInvalid
	}

	root, err := os.OpenRoot(canonical)
	if err != nil {
		return nil, errInvalid
	}
	defer func() { _ = root.Close() }()

	files, err := fs.Glob(root.FS(), "*.json")
	if err != nil || len(files) != pairCount*armCount {
		return nil, errInvalid
	}

	pairs := map[int]map[string]Report{}

	for _, filename := range files {
		report, readErr := readOneReport(root, filename)
		if readErr != nil || !validReport(report, expected) {
			return nil, errInvalid
		}

		if pairs[report.Pair] == nil {
			pairs[report.Pair] = map[string]Report{}
		}

		if _, duplicate := pairs[report.Pair][report.Arm]; duplicate {
			return nil, errInvalid
		}

		pairs[report.Pair][report.Arm] = report
	}

	return pairs, nil
}

func readOneReport(root *os.Root, filename string) (Report, error) {
	info, err := root.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaximumReportBytes {
		return Report{}, errInvalid
	}

	file, err := root.Open(filename)
	if err != nil {
		return Report{}, errInvalid
	}
	defer func() { _ = file.Close() }()

	return ReadReport(file)
}

func compatiblePair(baseline, candidate Report) bool {
	if baseline.Arm != baselineArm || candidate.Arm != candidateArm ||
		!reflect.DeepEqual(baseline.Environment, candidate.Environment) {
		return false
	}

	return sameProgram(baseline, candidate)
}

func sameProgram(baseline, candidate Report) bool {
	if baseline.Inputs != candidate.Inputs {
		return false
	}

	sort.Slice(
		baseline.Outputs,
		func(i, j int) bool { return baseline.Outputs[i].Target < baseline.Outputs[j].Target },
	)
	sort.Slice(
		candidate.Outputs,
		func(i, j int) bool { return candidate.Outputs[i].Target < candidate.Outputs[j].Target },
	)

	return reflect.DeepEqual(baseline.Outputs, candidate.Outputs)
}

func qualifies(baseline, candidate Report) bool {
	// Multiplication includes exactly 20%; subtracting ratios loses that boundary.
	const thresholdNumerator, thresholdDenominator = 4, 5

	return candidate.BinaryBuildSeconds*thresholdDenominator <= baseline.BinaryBuildSeconds*thresholdNumerator &&
		candidate.ElapsedSeconds <= baseline.ElapsedSeconds &&
		candidate.Resources.SwapOutPages <= baseline.Resources.SwapOutPages &&
		candidate.Resources.MemoryPeakBytes <= baseline.Resources.MemoryPeakBytes
}

// WriteJSON emits only a normalized observation, never an input diagnostic.
func WriteJSON(filename string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("normalize experiment: %w", err)
	}

	return WriteFile(filename, append(data, '\n'))
}

// WriteFile confines writes to the canonical output's existing parent directory.
func WriteFile(filename string, data []byte) error {
	const permissions = 0o600

	canonical, err := fsutil.EvalCanonicalPath(filename)
	if err != nil {
		return errInvalid
	}

	root, err := os.OpenRoot(filepath.Dir(canonical))
	if err != nil {
		return errInvalid
	}
	defer func() { _ = root.Close() }()

	err = root.WriteFile(filepath.Base(canonical), data, permissions)
	if err != nil {
		return fmt.Errorf("write experiment: %w", err)
	}

	return nil
}

// CheckCredentials refuses publication-bearing inputs without printing them.
func CheckCredentials() error {
	for _, name := range []string{
		"GH_TOKEN", "GITHUB_TOKEN", "HOMEBREW_GITHUB_API_TOKEN", "GPG_PRIVATE_KEY",
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "DOCKER_PASSWORD", "GHCR_TOKEN",
	} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return errInvalid
		}
	}

	return nil
}
