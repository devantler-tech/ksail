package cmd

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"devantler.tech/ksail/internal/ui/notify"
	"github.com/spf13/cobra"
)

//go:embed assets/ascii-art.txt
var asciiArt string

var rootCmd = &cobra.Command{
	Use:   "ksail",
	Short: "Spin up K8s clusters + ship workloads fast.",
	Long: `KSail is an SDK for operating and managing Kubernetes clusters and workloads.

  Create ephemeral clusters for development and CI purposes, deploy and update workloads, test and validate behavior — all through one concise, declarative interface. Stop stitching together a dozen CLIs; KSail gives you a consistent UX built on the tools you already trust.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		printAsciiArt()
		return cmd.Help()
	},
}

// SetVersionInfo sets the version string displayed by the root command.
func SetVersionInfo(version, commit, date string) {
	rootCmd.Version = fmt.Sprintf("%s (Built on %s from Git SHA %s)", version, date, commit)
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		notify.Errorf("%s", err)
		os.Exit(1)
	}
}

func printAsciiArt() {
	lines := strings.Split(asciiArt, "\n")
	for i, line := range lines {
		if i < 4 {
			fmt.Println("\x1b[1;33m" + line + "\x1b[0m")
		} else if i == 4 {
			fmt.Println("\x1b[1;34m" + line + "\x1b[0m")
		} else if i > 4 && i < 7 {
			// Add bounds checks to avoid panics if ascii-art changes
			if len(line) >= 38 {
				fmt.Print("\x1b[1;32m" + line[:32] + "\x1b[0m")
				fmt.Print("\x1B[36m" + line[32:37] + "\x1b[0m")
				fmt.Print("\x1b[1;34m" + line[37:38] + "\x1b[0m")
				fmt.Println("\x1B[36m" + line[38:] + "\x1b[0m")
			} else {
				fmt.Println("\x1b[1;32m" + line + "\x1b[0m")
			}
		} else if i > 6 && i < len(lines)-2 {
			if len(line) >= 32 {
				fmt.Print("\x1b[1;32m" + line[:32] + "\x1b[0m")
				fmt.Println("\x1B[36m" + line[32:] + "\x1b[0m")
			} else {
				fmt.Println("\x1b[1;32m" + line + "\x1b[0m")
			}
		} else {
			fmt.Println("\x1b[1;34m" + line + "\x1b[0m")
		}
	}
}
