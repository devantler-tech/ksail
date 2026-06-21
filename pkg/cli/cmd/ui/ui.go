// Package ui implements the `ksail ui` command, which serves the KSail web UI and a local REST API
// (backed by the local cluster lifecycle) and opens it in the browser.
package ui

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/browser"
	"github.com/devantler-tech/ksail/v7/pkg/cli/uiserver"
	"github.com/spf13/cobra"
)

// openBrowser is the browser launcher, overridable so command tests run without a real browser.
//
//nolint:gochecknoglobals // Injected for testability (see ui_test.go).
var openBrowser = browser.Open

const uiLongDesc = `Open the KSail web UI to provision and manage clusters on your machine.

This starts a small local web server (bound to 127.0.0.1 only) that serves the KSail web UI and a
REST API backed by your local cluster lifecycle, then opens it in your browser. It is the same UI
the KSail operator serves in a cluster, but here it manages clusters locally via Docker.

The server runs until you press Ctrl+C.

Examples:
  ksail ui
  ksail ui --port 8080
  ksail ui --no-browser`

// NewUICmd creates the `ksail ui` command.
func NewUICmd() *cobra.Command {
	var (
		portFlag  int
		noBrowser bool
	)

	cmd := &cobra.Command{
		Use:          "ui",
		Short:        "Open the KSail web UI to manage local clusters",
		Long:         uiLongDesc,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		// Exclude from AI tool generation: this is a long-running, blocking server + browser command,
		// like `chat` and `mcp`.
		Annotations: map[string]string{
			annotations.AnnotationExclude: "true",
		},
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runUICmd(cmd, portFlag, noBrowser)
	}

	cmd.Flags().IntVar(&portFlag, "port", 0, "Port to serve the UI on (0 picks a free port)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not open the browser automatically")

	return cmd
}

func runUICmd(cmd *cobra.Command, port int, noBrowser bool) error {
	// Cancel the context on Ctrl+C / SIGTERM so the server shuts down gracefully.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, url, err := uiserver.Listen(ctx, port)
	if err != nil {
		return fmt.Errorf("start ui server: %w", err)
	}

	server := uiserver.NewServer()

	// Wire the Copilot-backed AI assistant (read-only) so the web UI's assistant panel works when
	// Copilot is configured; its subprocess is stopped on shutdown. The panel stays hidden otherwise.
	chatRunner := uiserver.AttachChat(server, cmd.Root())
	defer chatRunner.Close()

	// Print a machine-parseable line first so wrappers can discover the URL, then a friendly message.
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "KSAIL_UI_URL=%s\n", url)
	_, _ = fmt.Fprintf(
		cmd.OutOrStdout(),
		"KSail web UI available at %s (press Ctrl+C to stop)\n",
		url,
	)

	if !noBrowser {
		openErr := openBrowser(ctx, url)
		if openErr != nil {
			_, _ = fmt.Fprintf(
				cmd.ErrOrStderr(),
				"could not open browser automatically (%v); open %s manually\n",
				openErr,
				url,
			)
		}
	}

	serveErr := server.Serve(ctx, listener)
	if serveErr != nil {
		return fmt.Errorf("serve ui: %w", serveErr)
	}

	return nil
}
