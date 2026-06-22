package open

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/browser"
	"github.com/devantler-tech/ksail/v7/pkg/cli/clusterapi"
	"github.com/devantler-tech/ksail/v7/pkg/cli/uiserver"
	"github.com/devantler-tech/ksail/v7/pkg/svc/pluginsig"
	"github.com/devantler-tech/ksail/v7/pkg/svc/webchat"
	"github.com/spf13/cobra"
)

// openBrowser is the browser launcher, overridable so command tests run without a real browser.
//
//nolint:gochecknoglobals // Injected for testability (see export_test.go).
var openBrowser = browser.Open

const webLongDesc = `Open the KSail web UI to provision and manage clusters on your machine.

This starts a small local web server (bound to 127.0.0.1 only) that serves the KSail web UI and a
REST API backed by your local cluster lifecycle, then opens it in your browser. It is the same UI
the KSail operator serves in a cluster, but here it manages clusters locally via Docker.

The server runs until you press Ctrl+C.

Examples:
  ksail open web
  ksail open web --port 8080
  ksail open web --no-browser`

// NewWebCmd creates the `ksail open web` command.
func NewWebCmd() *cobra.Command {
	var (
		portFlag  int
		noBrowser bool
	)

	cmd := &cobra.Command{
		Use:          "web",
		Short:        "Open the KSail web UI to manage local clusters",
		Long:         webLongDesc,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		// Exclude from AI tool generation: this is a long-running, blocking server + browser command,
		// like `chat` and `mcp`.
		Annotations: map[string]string{
			annotations.AnnotationExclude: annotations.AnnotationValueTrue,
		},
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runWebCmd(cmd, portFlag, noBrowser)
	}

	cmd.Flags().IntVar(&portFlag, "port", 0, "Port to serve the UI on (0 picks a free port)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not open the browser automatically")

	return cmd
}

func runWebCmd(cmd *cobra.Command, port int, noBrowser bool) error {
	// Cancel the context on Ctrl+C / SIGTERM so the server shuts down gracefully.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, url, err := uiserver.Listen(ctx, port)
	if err != nil {
		return fmt.Errorf("start ui server: %w", err)
	}

	server := uiserver.NewServer()

	// Wire the Copilot-backed AI assistant (read-only) onto the local backend, using the root command for
	// tool generation. Kept in the command layer (not the shared uiserver) so the Copilot SDK stays out
	// of the desktop app, which reuses uiserver without the assistant. The panel stays hidden unless
	// Copilot is configured; the subprocess is stopped on shutdown.
	chatRunner := webchat.New(cmd.Root())
	if service, ok := server.Service.(*clusterapi.Service); ok {
		service.UseChat(chatRunner)
		// Wire cosign/sigstore plugin verification (the strongest install authenticity tier). Kept in the
		// command layer (not the shared uiserver/clusterapi) so the heavy sigstore-go dependency stays out
		// of the desktop app, which reuses clusterapi without it. The CLI binary already links sigstore-go
		// transitively, so this adds no new dependency here.
		service.UseCosignVerifier(pluginsig.New())
	}

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
