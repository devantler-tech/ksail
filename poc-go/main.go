package main

import (
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/poc-go/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}