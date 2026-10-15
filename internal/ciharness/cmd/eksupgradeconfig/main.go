// Command eksupgradeconfig prepares the disposable EKS system-test project.
package main

import (
	"log"
	"os"

	"github.com/devantler-tech/ksail/v7/internal/ciharness/eksupgrade"
)

// main prepares the disposable project using the workflow's path and starting version.
func main() {
	err := eksupgrade.Prepare(os.Getenv("KSAIL_EKS_WORKDIR"), os.Getenv("EKS_UPGRADE_FROM"))
	if err != nil {
		log.Fatal(err)
	}
}
