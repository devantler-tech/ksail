// Command imagefactory compares public Image Factory transports without cluster access.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/imagefactorytransport"
	factoryclient "github.com/siderolabs/image-factory/pkg/client"
	"github.com/siderolabs/image-factory/pkg/schematic"
)

const (
	factoryURL       = "https://factory.talos.dev"
	requestBudget    = 30 * time.Second
	trials           = 2
	diagnosticBudget = 15 * time.Minute
	controlFields    = 3
	exitUsage        = 2
)

type observation struct {
	Mode        string `json:"mode"`
	Trial       int    `json:"trial"`
	Outcome     string `json:"outcome"`
	DNSDone     bool   `json:"dnsDone"`
	Connected   bool   `json:"connected"`
	TLSStarted  bool   `json:"tlsStarted"`
	TLSVerified bool   `json:"tlsVerified"`
	FirstByte   bool   `json:"firstByte"`
	IDMatches   bool   `json:"idMatches"`
	Protocol    string `json:"protocol"`
	ElapsedMS   int64  `json:"elapsedMs"`
}

// register traces phase completion, never remote addresses, headers, or raw errors.
func register(ctx context.Context, baseURL string, httpClient *http.Client) observation {
	result := observation{}

	var observationMutex sync.Mutex

	trace := traceFor(&result, &observationMutex)
	started := time.Now()

	bounded, cancel := context.WithTimeout(httptrace.WithClientTrace(ctx, trace), requestBudget)
	defer cancel()

	client, err := factoryclient.New(baseURL, factoryclient.WithClient(*httpClient))
	if err != nil {
		return observation{Outcome: "client_configuration"}
	}

	input := schematic.Schematic{}

	expected, err := input.ID()
	if err != nil {
		return observation{Outcome: "input_configuration"}
	}

	schematicID, normalized, err := client.SchematicCreate(bounded, input)

	observationMutex.Lock()
	defer observationMutex.Unlock()

	result.ElapsedMS = time.Since(started).Milliseconds()
	result.Outcome = errorKind(err)
	if err != nil {
		return result
	}

	result.IDMatches = matchesSchematic(expected, schematicID, normalized)
	if !result.IDMatches {
		result.Outcome = "id_mismatch"
	} else if !result.TLSVerified {
		result.Outcome = "unverified_tls"
	}

	return result
}

func traceFor(result *observation, observationMutex *sync.Mutex) *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSDone: func(info httptrace.DNSDoneInfo) {
			observationMutex.Lock()
			defer observationMutex.Unlock()

			result.DNSDone = info.Err == nil
		},
		ConnectDone: func(_, _ string, err error) {
			observationMutex.Lock()
			defer observationMutex.Unlock()

			result.Connected = result.Connected || err == nil
		},
		TLSHandshakeStart: func() {
			observationMutex.Lock()
			defer observationMutex.Unlock()

			result.TLSStarted = true
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			observationMutex.Lock()
			defer observationMutex.Unlock()

			result.TLSVerified = err == nil && len(state.VerifiedChains) > 0
			if err == nil {
				result.Protocol = negotiatedProtocol(state)
			}
		},
		GotFirstResponseByte: func() {
			observationMutex.Lock()
			defer observationMutex.Unlock()

			result.FirstByte = true
		},
	}
}

func negotiatedProtocol(state tls.ConnectionState) string {
	if state.NegotiatedProtocol == "h2" {
		return "h2"
	}

	return "http/1.1"
}

func matchesSchematic(expected, schematicID string, normalized *schematic.Schematic) bool {
	if normalized == nil || schematicID != expected {
		return false
	}

	normalizedID, err := normalized.ID()

	return err == nil && normalizedID == expected
}

// errorKind deliberately drops error messages, which may contain network details.
func errorKind(err error) string {
	if err == nil {
		return "ok"
	}

	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}

	var (
		certErr      *tls.CertificateVerificationError
		authorityErr x509.UnknownAuthorityError
	)

	if errors.As(err, &certErr) || errors.As(err, &authorityErr) {
		return "certificate"
	}

	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return "network_timeout"
	}

	return "request_error"
}

// transportFor changes one independent setting, retaining normal TLS verification.
func transportFor(mode string) *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil
	}

	transport := base.Clone()

	switch mode {
	case "http1":
		transport.Protocols = new(http.Protocols)
		transport.Protocols.SetHTTP1(true)
	case "pre126-curves":
		transport.TLSClientConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			CurvePreferences: []tls.CurveID{
				tls.X25519MLKEM768,
				tls.X25519,
				tls.CurveP256,
				tls.CurveP384,
				tls.CurveP521,
			},
		}
	case "classical-control":
		// Public synthetic comparison only; never a proposed production fallback.
		transport.TLSClientConfig = &tls.Config{
			MinVersion:       tls.VersionTLS12,
			CurvePreferences: []tls.CurveID{tls.X25519},
		}
	case "ipv4":
		dialer := &net.Dialer{Timeout: requestBudget, KeepAlive: requestBudget}
		transport.DialContext = func(ctx context.Context, _, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", address)
		}
	case "small-writes":
		return imagefactorytransport.New(transport)
	}

	return transport
}

// curlControl sends the very same synthetic body immediately before each SDK request.
func curlControl(ctx context.Context) observation {
	input := schematic.Schematic{}

	payload, err := input.Marshal()
	if err != nil {
		return observation{Outcome: "input_configuration"}
	}

	expected, err := input.ID()
	if err != nil {
		return observation{Outcome: "input_configuration"}
	}

	bounded, cancel := context.WithTimeout(ctx, requestBudget+time.Second)
	defer cancel()

	started := time.Now()
	cmd := curlCommand(bounded, payload)
	output, err := cmd.Output()

	result := observation{
		Mode:      "curl",
		Outcome:   errorKind(err),
		ElapsedMS: time.Since(started).Milliseconds(),
	}
	if err != nil {
		return result
	}

	return parseCurlControl(output, expected, result)
}

func curlCommand(ctx context.Context, payload []byte) *exec.Cmd {
	cmd := exec.CommandContext(
		ctx,
		"curl",
		"--disable",
		"--silent",
		"--show-error",
		"--fail",
		"--connect-timeout",
		"15",
		"--max-time",
		"30",
		"--header",
		"Content-Type: application/yaml",
		"--data-binary",
		"@-",
		"--write-out",
		"\n%{ssl_verify_result}\n%{http_code}",
		factoryURL+"/schematics",
	)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stderr = io.Discard

	return cmd
}

func parseCurlControl(output []byte, expected string, result observation) observation {
	parts := strings.Split(string(output), "\n")
	if len(parts) < controlFields {
		result.Outcome = "invalid_control"

		return result
	}

	result.TLSVerified = parts[len(parts)-2] == "0"

	var response struct {
		ID string `json:"id"`
	}
	if json.Unmarshal([]byte(strings.Join(parts[:len(parts)-2], "\n")), &response) != nil {
		result.Outcome = "invalid_control"

		return result
	}

	result.IDMatches = response.ID == expected
	if !result.TLSVerified {
		result.Outcome = "unverified_tls"
	} else if !result.IDMatches {
		result.Outcome = "id_mismatch"
	}

	return result
}

func main() {
	os.Exit(run())
}

func clientFor(mode string) *http.Client {
	if mode == "small-writes" {
		client := imagefactorytransport.Client()

		return &client
	}

	if mode == "sdk-default" {
		return &http.Client{}
	}

	transport := transportFor(mode)
	if transport == nil {
		return nil
	}

	return &http.Client{Transport: transport}
}

func run() int {
	if len(os.Args) != 1 {
		return exitUsage
	}

	ctx, cancel := context.WithTimeout(context.Background(), diagnosticBudget)
	defer cancel()

	encoder := json.NewEncoder(os.Stdout)

	for trial := 1; trial <= trials; trial++ {
		for _, mode := range []string{
			"sdk-default", "http1", "pre126-curves", "ipv4", "small-writes", "classical-control",
		} {
			control := curlControl(ctx)

			control.Trial = trial
			err := encoder.Encode(control)
			if err != nil {
				return 1
			}

			client := clientFor(mode)
			if client == nil {
				return 1
			}

			result := register(ctx, factoryURL, client)
			result.Mode, result.Trial = mode, trial

			client.CloseIdleConnections()

			err = encoder.Encode(result)
			if err != nil {
				return 1
			}

			if ctx.Err() != nil {
				return 1
			}
		}
	}
	// Completion means every observation was collected, not that production is fixed.
	err := encoder.Encode(
		map[string]string{"diagnostic": "completed", "productionRepair": "unverified"},
	)
	if err != nil {
		return 1
	}

	return 0
}
