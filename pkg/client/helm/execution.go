package helm

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// stderrCaptureMu protects process-wide stderr redirection from concurrent access.
var stderrCaptureMu sync.Mutex //nolint:gochecknoglobals // global lock required to coordinate stderr interception

func executeAndExtractRelease(
	runFn func() (any, error),
	silent bool,
) (*v1.Release, error) {
	var releaser any

	var err error

	if silent {
		releaser, err = runWithSilencedStderr(runFn)
	} else {
		releaser, err = runFn()
	}

	if err != nil {
		return nil, err
	}

	rel, ok := releaser.(*v1.Release)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errUnexpectedReleaseType, releaser)
	}

	return rel, nil
}

func releaseToInfo(rel *v1.Release) *ReleaseInfo {
	if rel == nil {
		return nil
	}

	return &ReleaseInfo{
		Name:       rel.Name,
		Namespace:  rel.Namespace,
		Revision:   rel.Version,
		Status:     rel.Info.Status.String(),
		Chart:      rel.Chart.Metadata.Name,
		AppVersion: rel.Chart.Metadata.AppVersion,
		Updated:    rel.Info.LastDeployed,
		Notes:      rel.Info.Notes,
	}
}

func runWithSilencedStderr(
	operation func() (any, error),
) (any, error) {
	readPipe, writePipe, pipeErr := os.Pipe()
	if pipeErr != nil {
		return operation()
	}

	stderrCaptureMu.Lock()
	defer stderrCaptureMu.Unlock()

	originalStderr := os.Stderr

	var (
		stderrBuffer bytes.Buffer
		waitGroup    sync.WaitGroup
	)

	waitGroup.Go(func() {
		_, _ = io.Copy(&stderrBuffer, readPipe)
	})

	os.Stderr = writePipe

	var (
		releaseResult any
		runErr        error
	)

	defer func() {
		_ = writePipe.Close()

		waitGroup.Wait()

		_ = readPipe.Close()
		os.Stderr = originalStderr

		if runErr != nil {
			logs := strings.TrimSpace(stderrBuffer.String())
			if logs != "" {
				runErr = fmt.Errorf("%w: %s", runErr, logs)
			}
		}
	}()

	releaseResult, runErr = operation()

	return releaseResult, runErr
}

