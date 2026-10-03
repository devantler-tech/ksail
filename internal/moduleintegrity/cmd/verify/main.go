// Command verify checks an extracted module against its authenticated source checksum.
package main

import (
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/internal/moduleintegrity"
)

func main() {
	const (
		argumentCount = 4
		usageExitCode = 2
	)

	if len(os.Args) != argumentCount {
		_, _ = fmt.Fprintln(os.Stderr, "usage: verify DIRECTORY MODULE@VERSION CHECKSUM")

		os.Exit(usageExitCode)
	}

	err := moduleintegrity.Verify(os.Args[1], os.Args[2], os.Args[3])
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)

		os.Exit(1)
	}
}
