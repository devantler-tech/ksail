// Command eksauthpreflight validates native STS output for a read-only trial.
package main

import (
	"log"
	"os"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/ciharness/eksauth"
)

func main() {
	err := eksauth.Verify(os.Stdin, os.Getenv("AWS_OIDC_ROLE_ARN"),
		os.Getenv("AWS_PREFLIGHT_SESSION"), os.Getenv("AWS_SESSION_EXPIRATION"), time.Now())
	if err != nil {
		log.Fatal(err)
	}

	log.Print("PASS: read-only OIDC identity and session lifetime verified")
}
