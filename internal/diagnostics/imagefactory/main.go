// Command imagefactory compares public Image Factory transports without cluster access.
package main

import (
	"context"
	"net/http"
)

type observation struct {
	Mode        string `json:"mode"`
	Outcome     string `json:"outcome"`
	TLSVerified bool   `json:"tlsVerified"`
	IDMatches   bool   `json:"idMatches"`
}

func register(_ context.Context, _ string, _ *http.Client) observation {
	return observation{Outcome: "not_implemented"}
}

func main() {}
