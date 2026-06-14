// Package desktop implements the `ksail desktop` command, which opens the KSail desktop application
// (a native window built from the same web UI). The desktop app is a separate binary; this command
// locates and launches it.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/spf13/cobra"
)

const (
	desktopBinaryName = "ksail-desktop"
	macAppName        = "KSail"
	osWindows         = "windows"
	osDarwin          = "darwin"
)

// errDesktopAppNotFound is returned when the KSail desktop application cannot be located.
var errDesktopAppNotFound = errors.New("KSail desktop app not found")

const desktopLongDesc = `Open the KSail desktop application.

The desktop app is a native window that runs the KSail web UI locally, letting you provision and
manage clusters without a browser. It ships as a separate download (or build it from source with
` + "`make desktop`" + `).

This command launches the desktop app from, in order: a "ksail-desktop" binary next to the ksail
executable, the same binary on your PATH, or (on macOS) an installed "KSail" app.`

// NewDesktopCmd creates the `ksail desktop` command.
func NewDesktopCmd(_ *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "desktop",
		Short:        "Open the KSail desktop application",
		Long:         desktopLongDesc,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		// Exclude from AI tool generation: this launches an interactive desktop app.
		Annotations: map[string]string{
			annotations.AnnotationExclude: "true",
		},
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return defaultLauncher().launch(cmd.Context(), cmd.OutOrStdout())
	}

	return cmd
}

// launcher locates and starts the desktop application. Its fields are injected so the resolution and
// launch behavior can be unit-tested without a real desktop binary.
type launcher struct {
	goos       string
	executable func() (string, error)
	lookPath   func(string) (string, error)
	fileExists func(string) bool
	start      func(*exec.Cmd) error
	run        func(*exec.Cmd) error
}

func defaultLauncher() launcher {
	return launcher{
		goos:       runtime.GOOS,
		executable: os.Executable,
		lookPath:   exec.LookPath,
		fileExists: fileExists,
		start:      (*exec.Cmd).Start,
		run:        (*exec.Cmd).Run,
	}
}

// launch finds the desktop application and starts it (see NewDesktopCmd's long description for the
// search order).
func (l launcher) launch(ctx context.Context, out io.Writer) error {
	binaryName := desktopBinaryName
	if l.goos == osWindows {
		binaryName += ".exe"
	}

	exe, exeErr := l.executable()
	if exeErr == nil {
		candidate := filepath.Join(filepath.Dir(exe), binaryName)
		if l.fileExists(candidate) {
			return l.startBinary(ctx, out, candidate)
		}
	}

	path, lookErr := l.lookPath(binaryName)
	if lookErr == nil {
		return l.startBinary(ctx, out, path)
	}

	if l.goos == osDarwin {
		openErr := l.openMacApp(ctx)
		if openErr == nil {
			_, _ = fmt.Fprintln(out, "Launched the KSail desktop app.")

			return nil
		}
	}

	return fmt.Errorf(
		"%w: build it with `make desktop` and put %s on your PATH, or install the KSail desktop app",
		errDesktopAppNotFound,
		desktopBinaryName,
	)
}

func (l launcher) startBinary(ctx context.Context, out io.Writer, path string) error {
	cmd := exec.CommandContext(ctx, path)

	err := l.start(cmd)
	if err != nil {
		return fmt.Errorf("launch desktop app %q: %w", path, err)
	}

	_, _ = fmt.Fprintf(out, "Launched the KSail desktop app (%s).\n", path)

	return nil
}

func (l launcher) openMacApp(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "open", "-a", macAppName)

	err := l.run(cmd)
	if err != nil {
		return fmt.Errorf("open -a %s: %w", macAppName, err)
	}

	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return !info.IsDir()
}
