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

	factoryclient "github.com/siderolabs/image-factory/pkg/client"
	"github.com/siderolabs/image-factory/pkg/schematic"
)

const (
	factoryURL    = "https://factory.talos.dev"
	requestBudget = 30 * time.Second
	trials        = 2
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
	var mu sync.Mutex
	trace := &httptrace.ClientTrace{
		DNSDone: func(info httptrace.DNSDoneInfo) { mu.Lock(); defer mu.Unlock(); result.DNSDone = info.Err == nil },
		ConnectDone: func(_, _ string, err error) {
			mu.Lock()
			defer mu.Unlock()
			result.Connected = result.Connected || err == nil
		},
		TLSHandshakeStart: func() { mu.Lock(); defer mu.Unlock(); result.TLSStarted = true },
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			mu.Lock()
			defer mu.Unlock()
			result.TLSVerified = err == nil && len(state.VerifiedChains) > 0
			if err == nil {
				result.Protocol = "http/1.1"
				if state.NegotiatedProtocol == "h2" {
					result.Protocol = "h2"
				}
			}
		},
		GotFirstResponseByte: func() { mu.Lock(); defer mu.Unlock(); result.FirstByte = true },
	}
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
	id, normalized, err := client.SchematicCreate(bounded, input)
	mu.Lock()
	defer mu.Unlock()
	result.ElapsedMS = time.Since(started).Milliseconds()
	result.Outcome = errorKind(err)
	if err != nil {
		return result
	}
	result.IDMatches = id == expected
	if normalized == nil {
		result.IDMatches = false
	} else {
		normalizedID, idErr := normalized.ID()
		result.IDMatches = result.IDMatches && idErr == nil && normalizedID == expected
	}
	if !result.IDMatches {
		result.Outcome = "id_mismatch"
	} else if !result.TLSVerified {
		result.Outcome = "unverified_tls"
	}
	return result
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
	var certErr *tls.CertificateVerificationError
	var authorityErr x509.UnknownAuthorityError
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
	transport := http.DefaultTransport.(*http.Transport).Clone()
	switch mode {
	case "http1":
		transport.Protocols = new(http.Protocols)
		transport.Protocols.SetHTTP1(true)
	case "pre126-curves":
		transport.TLSClientConfig = &tls.Config{
			MinVersion:       tls.VersionTLS12,
			CurvePreferences: []tls.CurveID{tls.X25519MLKEM768, tls.X25519, tls.CurveP256, tls.CurveP384, tls.CurveP521},
		}
	case "ipv4":
		dialer := &net.Dialer{Timeout: requestBudget, KeepAlive: requestBudget}
		transport.DialContext = func(ctx context.Context, _, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", address)
		}
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
	cmd := exec.CommandContext(bounded, "curl", "--silent", "--show-error", "--fail",
		"--connect-timeout", "15", "--max-time", "30", "--header", "Content-Type: application/yaml",
		"--data-binary", "@-", "--write-out", "\n%{ssl_verify_result}\n%{http_code}", factoryURL+"/schematics")
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stderr = io.Discard
	output, err := cmd.Output()
	result := observation{Mode: "curl", Outcome: errorKind(err), ElapsedMS: time.Since(started).Milliseconds()}
	if err != nil {
		return result
	}
	parts := strings.Split(string(output), "\n")
	if len(parts) < 3 {
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
	if len(os.Args) != 1 {
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	encoder := json.NewEncoder(os.Stdout)
	for trial := 1; trial <= trials; trial++ {
		for _, mode := range []string{"sdk-default", "http1", "pre126-curves", "ipv4"} {
			control := curlControl(ctx)
			control.Trial = trial
			if encoder.Encode(control) != nil {
				os.Exit(1)
			}
			client := &http.Client{}
			if mode != "sdk-default" {
				client.Transport = transportFor(mode)
			}
			result := register(ctx, factoryURL, client)
			result.Mode, result.Trial = mode, trial
			client.CloseIdleConnections()
			if encoder.Encode(result) != nil {
				os.Exit(1)
			}
			if ctx.Err() != nil {
				os.Exit(1)
			}
		}
	}
	// Completion means every observation was collected, not that production is fixed.
	if encoder.Encode(map[string]string{"diagnostic": "completed", "productionRepair": "unverified"}) != nil {
		os.Exit(1)
	}
}
