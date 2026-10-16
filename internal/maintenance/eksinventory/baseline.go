package eksinventory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"time"
)

const (
	maxBaselineBytes        = 16 << 20
	baselineMetadataReserve = 16 << 10
	baselineUnknown         = "unknown"
)

// BaselineContext binds a local inventory snapshot to one observation run.
// The supplied context does not prove account identity or resource ownership.
type BaselineContext struct {
	Region      string `json:"region"`
	ClusterName string `json:"clusterName"`
	RunID       string `json:"runId"`
	RunAttempt  string `json:"runAttempt"`
	SourceSHA   string `json:"sourceSha"`
}

type baselineManifest struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Repository    string                `json:"repository"`
	Context       BaselineContext       `json:"context"`
	StartedAt     time.Time             `json:"startedAt"`
	CompletedAt   time.Time             `json:"completedAt"`
	Coverage      map[string]string     `json:"coverage"`
	Observations  []baselineObservation `json:"observations"`
}

type baselineObservation struct {
	Service     string    `json:"service"`
	Operation   string    `json:"operation"`
	Complete    bool      `json:"complete"`
	Identities  *[]string `json:"identities,omitempty"`
	ReportCount *int      `json:"reportCount,omitempty"`
	Reason      string    `json:"reason,omitempty"`
}

// CaptureBaseline writes a new private exclusion snapshot and an aggregate summary.
// Failed families retain no identities and return ErrInventory after publication.
// Even successful capture proves neither ownership nor a completed teardown.
func CaptureBaseline(
	ctx context.Context,
	binding BaselineContext,
	destination string,
	output io.Writer,
	query Query,
) error {
	err := binding.Validate()
	if err != nil {
		return err
	}

	root, name, err := baselineDestination(destination)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	if query == nil {
		query = awsQuery
	}

	manifest, complete := collectBaseline(ctx, binding, query)

	data, err := json.Marshal(manifest)
	if err != nil || len(data) > maxBaselineBytes {
		return fmt.Errorf("baseline evidence limit exceeded: %w", ErrInventory)
	}

	err = publishBaseline(root, name, data)
	if err != nil {
		return err
	}

	err = printBaseline(output, manifest)
	if err != nil {
		return err
	}

	if !complete {
		return fmt.Errorf("baseline contains unknown observations: %w", ErrInventory)
	}

	return nil
}

// Validate rejects incomplete or malformed observation context before AWS reads.
func (binding BaselineContext) Validate() error {
	patterns := [][2]string{
		{binding.Region, `^[a-z]{2}(-[a-z]+)+-[0-9]+$`},
		{binding.ClusterName, `^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$`},
		{binding.SourceSHA, `^[0-9a-f]{40}$`},
	}
	for _, pair := range patterns {
		valid, _ := regexp.MatchString(pair[1], pair[0])
		if !valid {
			return fmt.Errorf("invalid baseline context: %w", ErrInventory)
		}
	}

	for _, value := range []string{binding.RunID, binding.RunAttempt} {
		number, err := strconv.ParseUint(value, 10, 64)
		if err != nil || number == 0 || strconv.FormatUint(number, 10) != value {
			return fmt.Errorf("invalid baseline run identity: %w", ErrInventory)
		}
	}

	return nil
}

func collectBaseline(
	ctx context.Context,
	binding BaselineContext,
	query Query,
) (baselineManifest, bool) {
	manifest := baselineManifest{
		SchemaVersion: 1, Repository: "devantler-tech/ksail", Context: binding,
		StartedAt: time.Now().UTC(), Coverage: map[string]string{
			"ownership":      baselineUnknown,
			"teardown":       "unverified",
			"instantFleets":  baselineUnknown,
			"accountBinding": baselineUnknown,
			"retention":      "local-file",
		},
	}
	complete := true

	remainingBytes := maxBaselineBytes - baselineMetadataReserve
	for _, spec := range operations() {
		observed, err := collect(ctx, binding.Region, spec, query, remainingBytes)

		family := baselineFamily(spec, observed, err)
		if family.Complete {
			remainingBytes -= observed.identityBytes
		} else {
			complete = false
		}

		manifest.Observations = append(manifest.Observations, family)
	}

	manifest.CompletedAt = time.Now().UTC()

	return manifest, complete
}

func baselineFamily(
	spec operation,
	observed observation,
	readErr error,
) baselineObservation {
	family := baselineObservation{Service: spec.service, Operation: spec.command}
	if readErr != nil {
		family.Reason = failureReason(readErr)

		return family
	}

	identities := make([]string, 0, len(observed.identities))
	for identity := range observed.identities {
		identities = append(identities, identity)
	}

	sort.Strings(identities)

	family.Complete = true
	family.Identities = &identities
	family.ReportCount = &observed.count

	return family
}

func printBaseline(output io.Writer, manifest baselineManifest) error {
	_, err := fmt.Fprintf(
		output,
		"EKS regional baseline (%s)\n",
		manifest.Context.Region,
	)
	if err != nil {
		return fmt.Errorf("write baseline summary: %w", err)
	}

	for _, family := range manifest.Observations {
		var err error
		if family.Complete {
			_, err = fmt.Fprintf(output, "%s %s: observed=%d report-count=%d\n",
				family.Service, family.Operation, len(*family.Identities), *family.ReportCount)
		} else {
			_, err = fmt.Fprintf(
				output,
				"%s %s: unknown (%s)\n",
				family.Service,
				family.Operation,
				family.Reason,
			)
		}

		if err != nil {
			return fmt.Errorf("write baseline family: %w", err)
		}
	}

	_, err = fmt.Fprintln(
		output,
		"ownership=unknown account-binding=unknown instant-fleets=unknown teardown=unverified retention=local-file\n"+
			"New identities do not establish run ownership. This observation cannot certify another run.",
	)
	if err != nil {
		return fmt.Errorf("write baseline coverage: %w", err)
	}

	return nil
}
