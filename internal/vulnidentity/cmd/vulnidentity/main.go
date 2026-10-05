// Command vulnidentity checks published vulnerability identities of local source replacements.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/vulnidentity"
)

func main() {
	root := flag.String("root", ".", "repository root")
	scanner := flag.String("scanner", "govulncheck", "installed govulncheck v1.8.0 executable")

	flag.Parse()

	err := run(*root, *scanner)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(root, scanner string) error {
	const queryTimeout = 10 * time.Minute

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	identities, err := vulnidentity.Collect(ctx, root)
	if err != nil {
		return fmt.Errorf("collect published identities: %w", err)
	}

	if len(identities) == 0 {
		_, err = fmt.Fprintln(
			os.Stdout,
			"No local module replacements require complementary scanning.",
		)
		if err != nil {
			return fmt.Errorf("write identity scan result: %w", err)
		}

		return nil
	}

	findings, err := vulnidentity.Query(ctx, scanner, "", identities)
	if err != nil {
		return fmt.Errorf("query published identities: %w", err)
	}

	if len(findings) > 0 {
		return fmt.Errorf("%w: %s", vulnidentity.ErrAdvisories, strings.Join(findings, ", "))
	}

	_, err = fmt.Fprintf(
		os.Stdout,
		"Checked %d published local-source identities with govulncheck v1.8.0.\n",
		len(identities),
	)
	if err != nil {
		return fmt.Errorf("write identity scan result: %w", err)
	}

	return nil
}
