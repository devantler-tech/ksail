// Package hetznertalos keeps KSail's Hetzner public-ISO compatibility inputs
// synchronized with Hetzner's official changelog feed.
package hetznertalos

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

const (
	// DefaultFeedURL is Hetzner's official machine-readable changelog feed.
	DefaultFeedURL    = "https://docs.hetzner.cloud/changelog/feed.rss"
	maxFeedBytes      = 4 << 20
	httpTimeout       = 30 * time.Second
	expectedFileCount = 2
)

var (
	errNoAnnouncement        = errors.New("no Talos ISO announcement")
	errMalformedAnnouncement = errors.New("malformed Talos ISO announcement")
	errUnexpectedSource      = errors.New("unexpected Talos ISO announcement source")
	errUnexpectedFeedStatus  = errors.New("unexpected Hetzner changelog status")
	errInvalidISOID          = errors.New("invalid Talos ISO ID")
	errDuplicateISOID        = errors.New("talos amd64 and arm64 ISO IDs must differ")
	errConflictingRelease    = errors.New("conflicting Talos ISO announcements")
	errSourceFieldMissing    = errors.New("coupled source field missing")
	errUnexpectedSourceFiles = errors.New("unexpected coupled source-file count")
	errMachineryMissing      = errors.New("versioned Talos machinery requirement missing")
	errMachineryTooOld       = errors.New("talos machinery is older than Hetzner ISO")
	errUnexpectedSourceMatch = errors.New("unexpected coupled source match count")

	talosTitlePattern = regexp.MustCompile(
		`^Talos Linux v([0-9]+\.[0-9]+\.[0-9]+) ISO now available$`,
	)
	talosContentPattern = regexp.MustCompile(
		`(?s)Talos Linux\s+([0-9]+\.[0-9]+\.[0-9]+).*?IDs\s*` +
			`<code>([0-9]+)</code>\s*\(x86\)\s*(?:&#x26;|&amp;|&)\s*` +
			`<code>([0-9]+)</code>\s*\(arm\)`,
	)
	defaultTalosVersionPattern = regexp.MustCompile(
		`(DefaultHetznerTalosVersion\s*=\s*")v([0-9]+\.[0-9]+\.[0-9]+)(")`,
	)
	defaultISOIDPattern = regexp.MustCompile(
		`(DefaultTalosISO\s+int64\s*=\s*)([0-9]+)`,
	)
	isoStructTagPattern = regexp.MustCompile(
		"(ISO\\s+int64\\s+`default:\")[0-9]+(\" json:\"iso,omitzero\"`)",
	)
	machineryRequirementPattern = regexp.MustCompile(
		`(?m)^\s*(?:require\s+)?github\.com/siderolabs/talos/pkg/machinery\s+(v\S+)(?:\s+//.*)?$`,
	)
	machineryReplacementPattern = regexp.MustCompile(
		`(?m)^\s*(?:replace\s+)?github\.com/siderolabs/talos/pkg/machinery\s*=>\s*\S+(?:\s+(v\S+))?\s*$`,
	)
)

// Release is one Talos public ISO announcement in Hetzner's changelog.
type Release struct {
	Version  string
	ISOAMD64 int64
	ISOARM64 int64
	Source   string
}

type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	Content string `xml:"encoded"`
}

type sourceFile struct {
	path    string
	content []byte
	mode    os.FileMode
}

// Run loads the official feed (or a local fixture), applies a monotonic update,
// and reports whether the repository baseline changed.
func Run(ctx context.Context, root, feedFile string, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}

	feed, err := openFeed(ctx, feedFile)
	if err != nil {
		return err
	}

	defer func() { _ = feed.Close() }()

	latest, err := ParseLatest(feed)
	if err != nil {
		return err
	}

	currentVersion, currentISO, files, err := loadSources(root)
	if err != nil {
		return err
	}

	newer, err := isNewer(latest.Version, currentVersion)
	if err != nil {
		return err
	}

	if !newer {
		_, _ = fmt.Fprintf(
			out,
			"Hetzner Talos ISO already current at %s/%d\n",
			currentVersion,
			currentISO,
		)

		return nil
	}

	supportErr := ensureMachinerySupports(root, latest.Version)
	if supportErr != nil {
		return supportErr
	}

	updated, err := updateSources(files, latest)
	if err != nil {
		return err
	}

	err = writeSources(updated)
	if err != nil {
		return err
	}

	reportUpdate(out, currentVersion, currentISO, latest)

	return nil
}

func reportUpdate(out io.Writer, currentVersion string, currentISO int64, latest Release) {
	_, _ = fmt.Fprintf(
		out,
		"Updated Hetzner Talos ISO %s/%d -> v%s/%d (%s)\n",
		currentVersion,
		currentISO,
		latest.Version,
		latest.ISOAMD64,
		latest.Source,
	)
}

// ParseLatest returns the highest stable Talos ISO release announced in the
// feed. Matching malformed entries fail closed instead of being skipped.
func ParseLatest(reader io.Reader) (Release, error) {
	var feed rssFeed

	err := xml.NewDecoder(io.LimitReader(reader, maxFeedBytes)).Decode(&feed)
	if err != nil {
		return Release{}, fmt.Errorf("decode Hetzner changelog RSS: %w", err)
	}

	var (
		latestVersion *semver.Version
		latestKey     string
		latest        Release
	)

	releases := make(map[string]Release)
	conflicts := make(map[string]struct{})

	for _, item := range feed.Channel.Items {
		titleMatch := talosTitlePattern.FindStringSubmatch(strings.TrimSpace(item.Title))
		if titleMatch == nil {
			continue
		}

		release, parsedVersion, err := parseRelease(item, titleMatch[1])
		if err != nil {
			return Release{}, err
		}

		versionKey := parsedVersion.String()
		recordRelease(releases, conflicts, versionKey, release)

		if latestVersion == nil || parsedVersion.GreaterThan(latestVersion) {
			latest = release
			latestVersion = parsedVersion
			latestKey = versionKey
		}
	}

	if latestVersion == nil {
		return Release{}, fmt.Errorf("%w in Hetzner changelog", errNoAnnouncement)
	}

	if _, conflicted := conflicts[latestKey]; conflicted {
		return Release{}, fmt.Errorf("%w for v%s", errConflictingRelease, latest.Version)
	}

	return latest, nil
}

func recordRelease(
	releases map[string]Release,
	conflicts map[string]struct{},
	versionKey string,
	release Release,
) {
	existing, exists := releases[versionKey]
	if !exists {
		releases[versionKey] = release

		return
	}

	if existing.ISOAMD64 == release.ISOAMD64 && existing.ISOARM64 == release.ISOARM64 {
		return
	}

	conflicts[versionKey] = struct{}{}
}

func parseRelease(item rssItem, titleVersion string) (Release, *semver.Version, error) {
	contentMatch := talosContentPattern.FindStringSubmatch(item.Content)
	if contentMatch == nil {
		return Release{}, nil, fmt.Errorf(
			"%w: entry %q has an unrecognized ID payload",
			errMalformedAnnouncement,
			item.Title,
		)
	}

	if contentMatch[1] != titleVersion {
		return Release{}, nil, fmt.Errorf(
			"%w: entry %q disagrees with payload version %q",
			errMalformedAnnouncement,
			item.Title,
			contentMatch[1],
		)
	}

	if !strings.HasPrefix(item.Link, "https://docs.hetzner.cloud/changelog#") {
		return Release{}, nil, fmt.Errorf("%w: %q", errUnexpectedSource, item.Link)
	}

	parsedVersion, err := semver.NewVersion(titleVersion)
	if err != nil {
		return Release{}, nil, fmt.Errorf("parse Talos ISO version %q: %w", titleVersion, err)
	}

	amd64ID, err := strconv.ParseInt(contentMatch[2], 10, 64)
	if err != nil {
		return Release{}, nil, fmt.Errorf("parse Talos amd64 ISO ID %q: %w", contentMatch[2], err)
	}

	if amd64ID <= 0 {
		return Release{}, nil, fmt.Errorf("%w: amd64 %q", errInvalidISOID, contentMatch[2])
	}

	arm64ID, err := strconv.ParseInt(contentMatch[3], 10, 64)
	if err != nil {
		return Release{}, nil, fmt.Errorf("parse Talos arm64 ISO ID %q: %w", contentMatch[3], err)
	}

	if arm64ID <= 0 {
		return Release{}, nil, fmt.Errorf("%w: arm64 %q", errInvalidISOID, contentMatch[3])
	}

	if amd64ID == arm64ID {
		return Release{}, nil, errDuplicateISOID
	}

	return Release{
		Version:  titleVersion,
		ISOAMD64: amd64ID,
		ISOARM64: arm64ID,
		Source:   item.Link,
	}, parsedVersion, nil
}

func openFeed(ctx context.Context, feedFile string) (io.ReadCloser, error) {
	if strings.TrimSpace(feedFile) != "" {
		// The caller explicitly supplies this local fixture path.
		file, err := os.Open(feedFile) //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("open Hetzner changelog fixture: %w", err)
		}

		return file, nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, DefaultFeedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build Hetzner changelog request: %w", err)
	}

	response, err := (&http.Client{Timeout: httpTimeout}).Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch Hetzner changelog: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()

		return nil, fmt.Errorf("%w: HTTP %s", errUnexpectedFeedStatus, response.Status)
	}

	return response.Body, nil
}

func loadSources(root string) (string, int64, []sourceFile, error) {
	paths := []string{
		"pkg/apis/cluster/v1alpha1/defaults.go",
		"pkg/apis/cluster/v1alpha1/options.go",
	}
	files := make([]sourceFile, 0, len(paths))

	for _, name := range paths {
		path := filepath.Join(root, filepath.FromSlash(name))

		content, err := fsutil.ReadFileSafe(root, path)
		if err != nil {
			return "", 0, nil, fmt.Errorf("read source %s: %w", path, err)
		}

		info, err := os.Stat(path)
		if err != nil {
			return "", 0, nil, fmt.Errorf("stat source %s: %w", path, err)
		}

		files = append(files, sourceFile{path: path, content: content, mode: info.Mode().Perm()})
	}

	versionMatch := defaultTalosVersionPattern.FindSubmatch(files[0].content)
	if versionMatch == nil {
		return "", 0, nil, fmt.Errorf("%w: DefaultHetznerTalosVersion", errSourceFieldMissing)
	}

	isoMatch := defaultISOIDPattern.FindSubmatch(files[0].content)
	if isoMatch == nil {
		return "", 0, nil, fmt.Errorf("%w: DefaultTalosISO", errSourceFieldMissing)
	}

	isoID, err := strconv.ParseInt(string(isoMatch[2]), 10, 64)
	if err != nil {
		return "", 0, nil, fmt.Errorf("parse current DefaultTalosISO: %w", err)
	}

	return "v" + string(versionMatch[2]), isoID, files, nil
}

func isNewer(candidate, current string) (bool, error) {
	candidateVersion, err := semver.NewVersion(strings.TrimPrefix(candidate, "v"))
	if err != nil {
		return false, fmt.Errorf("parse candidate Talos version %q: %w", candidate, err)
	}

	currentVersion, err := semver.NewVersion(strings.TrimPrefix(current, "v"))
	if err != nil {
		return false, fmt.Errorf("parse current Talos version %q: %w", current, err)
	}

	return candidateVersion.GreaterThan(currentVersion), nil
}

func updateSources(files []sourceFile, release Release) ([]sourceFile, error) {
	if len(files) != expectedFileCount {
		return nil, fmt.Errorf("%w: got %d", errUnexpectedSourceFiles, len(files))
	}

	var err error

	files[0].content, err = replaceExactlyOnce(
		files[0].content,
		defaultTalosVersionPattern,
		fmt.Sprintf(`${1}v%s${3}`, release.Version),
		"DefaultHetznerTalosVersion",
	)
	if err != nil {
		return nil, err
	}

	files[0].content, err = replaceExactlyOnce(
		files[0].content,
		defaultISOIDPattern,
		fmt.Sprintf(`${1}%d`, release.ISOAMD64),
		"DefaultTalosISO",
	)
	if err != nil {
		return nil, err
	}

	files[1].content, err = replaceExactlyOnce(
		files[1].content,
		isoStructTagPattern,
		fmt.Sprintf(`${1}%d${2}`, release.ISOAMD64),
		"OptionsTalos.ISO default tag",
	)
	if err != nil {
		return nil, err
	}

	return files, nil
}

func writeSources(files []sourceFile) error {
	return writeSourcesWith(files, fsutil.AtomicWriteFile)
}

func writeSourcesWith(
	files []sourceFile,
	writeFile func(path string, content []byte, mode os.FileMode) error,
) error {
	backups := make([]sourceFile, len(files))
	for index, file := range files {
		original, err := fsutil.ReadFileSafe(filepath.Dir(file.path), file.path)
		if err != nil {
			return fmt.Errorf("stage original source %s: %w", file.path, err)
		}

		info, err := os.Stat(file.path)
		if err != nil {
			return fmt.Errorf("stat original source %s: %w", file.path, err)
		}

		backups[index] = sourceFile{path: file.path, content: original, mode: info.Mode().Perm()}
	}

	for index, file := range files {
		err := writeFile(file.path, file.content, file.mode)
		if err != nil {
			writeErr := fmt.Errorf("write updated source %s: %w", file.path, err)
			rollbackErrs := []error{writeErr}

			for rollbackIndex := index - 1; rollbackIndex >= 0; rollbackIndex-- {
				backup := backups[rollbackIndex]

				rollbackErr := writeFile(backup.path, backup.content, backup.mode)
				if rollbackErr != nil {
					rollbackErrs = append(
						rollbackErrs,
						fmt.Errorf("restore source %s: %w", backup.path, rollbackErr),
					)
				}
			}

			return errors.Join(rollbackErrs...)
		}
	}

	return nil
}

func ensureMachinerySupports(root, releaseVersion string) error {
	version, err := readMachineryVersion(root)
	if err != nil {
		return err
	}

	machineryVersion, err := semver.NewVersion(strings.TrimPrefix(version, "v"))
	if err != nil {
		return fmt.Errorf("parse Talos machinery version %q: %w", version, err)
	}

	release, err := semver.NewVersion(strings.TrimPrefix(releaseVersion, "v"))
	if err != nil {
		return fmt.Errorf("parse announced Talos version %q: %w", releaseVersion, err)
	}

	if machineryVersion.LessThan(release) {
		return fmt.Errorf(
			"%w: Hetzner announced v%s, embedded dependency is %s; update the dependency first",
			errMachineryTooOld,
			releaseVersion,
			version,
		)
	}

	return nil
}

func readMachineryVersion(root string) (string, error) {
	path := filepath.Join(root, "go.mod")

	content, err := fsutil.ReadFileSafe(root, path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	requirements := machineryRequirementPattern.FindAllSubmatch(content, -1)
	if len(requirements) != 1 {
		return "", fmt.Errorf(
			"%w: got %d requirements in %s",
			errMachineryMissing,
			len(requirements),
			path,
		)
	}

	version := string(requirements[0][1])

	replacements := machineryReplacementPattern.FindAllSubmatch(content, -1)
	if len(replacements) > 1 {
		return "", fmt.Errorf(
			"%w: got %d replacements in %s",
			errMachineryMissing,
			len(replacements),
			path,
		)
	}

	if len(replacements) == 1 {
		if len(replacements[0]) < 2 || len(replacements[0][1]) == 0 {
			return "", fmt.Errorf("%w: unversioned replacement in %s", errMachineryMissing, path)
		}

		version = string(replacements[0][1])
	}

	return version, nil
}

func replaceExactlyOnce(
	content []byte,
	pattern *regexp.Regexp,
	replacement, field string,
) ([]byte, error) {
	matches := pattern.FindAllIndex(content, -1)
	if len(matches) != 1 {
		return nil, fmt.Errorf("%w for %s: got %d", errUnexpectedSourceMatch, field, len(matches))
	}

	return pattern.ReplaceAll(content, []byte(replacement)), nil
}
