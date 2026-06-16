package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	sigsyaml "sigs.k8s.io/yaml"
)

// RestoreOptions configures a [Restorer]. The fields mirror the cluster restore
// CLI flags plus the resolved kubeconfig path.
type RestoreOptions struct {
	// KubeconfigPath is the resolved path to the kubeconfig used for restore.
	KubeconfigPath string
	// Context, when non-empty, pins the restore to a specific kubeconfig context
	// (e.g. resolved from --name) instead of the kubeconfig's current-context.
	Context string
	// InputPath is the backup archive to restore. Callers should canonicalize
	// it (e.g. via fsutil.EvalCanonicalPath) before invoking.
	InputPath string
	// ExistingResourcePolicy controls how existing resources are handled:
	// [PolicyNone] (skip) or [PolicyUpdate] (patch).
	ExistingResourcePolicy string
	// DryRun prints what would be restored without applying.
	DryRun bool
}

// Restorer restores cluster resources from a backup archive.
type Restorer struct {
	opts RestoreOptions
}

// NewRestorer constructs a Restorer from the given options.
func NewRestorer(opts RestoreOptions) *Restorer {
	return &Restorer{opts: opts}
}

// ExtractedArchive is the result of extracting a backup archive: a temp
// directory holding the resource manifests plus the parsed [BackupMetadata].
// Callers must invoke Cleanup when done.
type ExtractedArchive struct {
	// Dir is the temporary directory the archive was extracted into.
	Dir string
	// Metadata is the parsed archive metadata.
	Metadata *BackupMetadata
	// Cleanup removes the temporary directory.
	Cleanup func()
}

// Extract extracts the configured backup archive into a temporary directory and
// parses its [BackupMetadata]. The caller owns the returned Cleanup.
func (r *Restorer) Extract() (*ExtractedArchive, error) {
	tmpDir, metadata, err := extractBackupArchive(r.opts.InputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract backup: %w", err)
	}

	return &ExtractedArchive{
		Dir:      tmpDir,
		Metadata: metadata,
		Cleanup:  func() { _ = os.RemoveAll(tmpDir) },
	}, nil
}

// RestoreExtracted applies the resources from a previously extracted archive to
// the target cluster, writing per-resource progress to writer.
func (r *Restorer) RestoreExtracted(
	ctx context.Context,
	archive *ExtractedArchive,
	writer io.Writer,
) error {
	backupName := deriveBackupName(r.opts.InputPath)
	restoreName := fmt.Sprintf("restore-%d", time.Now().UTC().UnixNano())

	err := r.restoreResources(ctx, archive.Dir, writer, backupName, restoreName)
	if err != nil {
		return fmt.Errorf("failed to restore resources: %w", err)
	}

	return nil
}

// deriveBackupName extracts a human-readable backup name from the archive path.
func deriveBackupName(inputPath string) string {
	base := filepath.Base(inputPath)
	name := strings.TrimSuffix(base, ".tar.gz")
	name = strings.TrimSuffix(name, ".tgz")

	return name
}

func (r *Restorer) restoreResources(
	ctx context.Context,
	tmpDir string,
	writer io.Writer,
	backupName, restoreName string,
) error {
	resourcesDir := filepath.Join(tmpDir, "resources")

	var restoreErrors []string

	for _, resourceType := range backupResourceTypes() {
		errs, err := r.restoreResourceType(
			ctx, resourcesDir, resourceType,
			writer, backupName, restoreName,
		)
		if err != nil {
			return err
		}

		restoreErrors = append(restoreErrors, errs...)
	}

	if len(restoreErrors) > 0 {
		return fmt.Errorf(
			"%w: %d resource(s): %s",
			ErrRestoreFailed,
			len(restoreErrors),
			strings.Join(restoreErrors, "; "),
		)
	}

	return nil
}

// restoreResourceType restores all YAML files for a single resource type
// from the backup directory, returning any per-file errors.
func (r *Restorer) restoreResourceType(
	ctx context.Context,
	resourcesDir, resourceType string,
	writer io.Writer,
	backupName, restoreName string,
) ([]string, error) {
	resourceDir := filepath.Join(resourcesDir, resourceType)

	_, statErr := os.Stat(resourceDir)
	if os.IsNotExist(statErr) {
		return nil, nil
	}

	files, err := filepath.Glob(
		filepath.Join(resourceDir, "*.yaml"),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to list files for %s: %w", resourceType, err,
		)
	}

	if len(files) == 0 {
		return nil, nil
	}

	var errs []string

	for _, file := range files {
		err = r.restoreResourceFile(
			ctx, file, backupName, restoreName,
		)
		if err != nil {
			msg := fmt.Sprintf("%s: %v", filepath.Base(file), err)
			errs = append(errs, msg)

			_, _ = fmt.Fprintf(
				writer,
				"Warning: failed to restore %s: %v\n",
				filepath.Base(file), err,
			)

			continue
		}
	}

	// Only report success when every file for this type restored; partial
	// failures are surfaced via the per-file warnings above and the overall
	// ErrRestoreFailed the caller returns.
	if len(errs) == 0 {
		_, _ = fmt.Fprintf(writer, "   Restored %s\n", resourceType)
	}

	return errs, nil
}

func (r *Restorer) restoreResourceFile(
	ctx context.Context,
	filePath string,
	backupName, restoreName string,
) error {
	labeledPath, err := injectRestoreLabels(
		filePath, backupName, restoreName,
	)
	if err != nil {
		return fmt.Errorf("failed to inject labels: %w", err)
	}

	defer func() { _ = os.Remove(labeledPath) }()

	// Skip files with no Kubernetes objects (empty backup category).
	if isEmptyYAML(labeledPath) {
		return nil
	}

	var outBuf, errBuf bytes.Buffer

	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    &outBuf,
		ErrOut: &errBuf,
	}).WithKubeContext(r.opts.Context)

	var cmd *cobra.Command

	if r.opts.ExistingResourcePolicy == PolicyNone {
		cmd = client.CreateCreateCommand(r.opts.KubeconfigPath)
	} else {
		cmd = client.CreateApplyCommand(r.opts.KubeconfigPath)
	}

	args := []string{"-f", labeledPath}

	useServerSideApply := r.opts.ExistingResourcePolicy == PolicyUpdate
	if useServerSideApply {
		// Server-side apply avoids the client-side
		// last-applied-configuration annotation that can exceed the
		// 262144-byte annotation limit for large resources (e.g.
		// ArgoCD CRDs, Helm release Secrets).
		args = append(args, "--server-side", "--force-conflicts")
	}

	if r.opts.DryRun {
		if useServerSideApply {
			args = append(args, "--dry-run=server")
		} else {
			args = append(args, "--dry-run=client")
		}
	}

	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err = kubectl.ExecuteSafely(ctx, cmd)

	return classifyRestoreError(err, errBuf.String(), r.opts.ExistingResourcePolicy)
}

// isEmptyYAML returns true if the file at path contains no Kubernetes
// objects — only whitespace and YAML document separators.
func isEmptyYAML(path string) bool {
	data, err := os.ReadFile(path) //nolint:gosec // path from extracted temp dir
	if err != nil {
		return false
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && trimmed != "---" {
			return false
		}
	}

	return true
}

// classifyRestoreError returns nil for benign errors (already-existing
// resources) and wraps real failures. The policy argument selects the
// already-exists tolerance applied under [PolicyNone].
func classifyRestoreError(err error, stderr, policy string) error {
	if err == nil {
		return nil
	}

	if policy == PolicyNone {
		// Some resource types (e.g. DaemonSet, Job) route
		// "AlreadyExists" through BehaviorOnFatal instead of stderr.
		// Fall back to err.Error() when stderr is empty or
		// whitespace-only (which allLinesContain would also reject).
		source := stderr
		if strings.TrimSpace(source) == "" {
			source = err.Error()
		}

		if allLinesContain(source, "already exists") {
			return nil
		}
	}

	if stderr != "" {
		return fmt.Errorf("kubectl failed: %w (output: %s)", err, stderr)
	}

	return fmt.Errorf("kubectl failed: %w", err)
}

// injectRestoreLabels reads a YAML file, adds restore labels to each
// document, and writes the result to a temporary file. Returns the path
// to the temporary file.
func injectRestoreLabels(
	filePath, backupName, restoreName string,
) (string, error) {
	data, err := os.ReadFile(filePath) //nolint:gosec // path from extracted temp dir
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	docs := splitYAMLDocuments(string(data))

	var builder strings.Builder

	const estimatedBytesPerDoc = 512
	builder.Grow(len(docs) * estimatedBytesPerDoc)

	for idx, doc := range docs {
		if strings.TrimSpace(doc) == "" {
			continue
		}

		labeled, labelErr := addLabelsToDocument(
			doc, backupName, restoreName,
		)
		if labelErr != nil {
			return "", fmt.Errorf(
				"failed to inject restore labels: %w", labelErr,
			)
		}

		if idx > 0 {
			builder.WriteString("---\n")
		}

		builder.WriteString(labeled)
	}

	tmpFile, err := os.CreateTemp("", "ksail-restore-labeled-*.yaml")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	defer func() { _ = tmpFile.Close() }()

	_, err = tmpFile.WriteString(builder.String())
	if err != nil {
		_ = os.Remove(tmpFile.Name())

		return "", fmt.Errorf("failed to write labeled file: %w", err)
	}

	return tmpFile.Name(), nil
}

// addLabelsToDocument parses a single YAML document and adds restore labels.
func addLabelsToDocument(
	doc, backupName, restoreName string,
) (string, error) {
	var obj unstructured.Unstructured

	err := sigsyaml.Unmarshal([]byte(doc), &obj.Object)
	if err != nil {
		return "", fmt.Errorf("failed to parse YAML: %w", err)
	}

	if obj.Object == nil {
		return doc, nil
	}

	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	labels["ksail.io/backup-name"] = backupName
	labels["ksail.io/restore-name"] = restoreName
	obj.SetLabels(labels)

	result, err := sigsyaml.Marshal(obj.Object)
	if err != nil {
		return "", fmt.Errorf("failed to marshal YAML: %w", err)
	}

	return string(result), nil
}

// splitYAMLDocuments splits a multi-document YAML string into individual
// documents using the "---" separator.
func splitYAMLDocuments(content string) []string {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	var docs []string

	current := strings.Builder{}

	for line := range strings.SplitSeq(content, "\n") {
		if line == "---" {
			if current.Len() > 0 {
				docs = append(docs, current.String())
				current.Reset()
			}

			continue
		}

		current.WriteString(line)
		current.WriteString("\n")
	}

	if current.Len() > 0 {
		docs = append(docs, current.String())
	}

	return docs
}

func allLinesContain(output, substr string) bool {
	hasNonEmptyLine := false

	for line := range strings.SplitSeq(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		hasNonEmptyLine = true

		if !strings.Contains(trimmed, substr) {
			return false
		}
	}

	return hasNonEmptyLine
}
