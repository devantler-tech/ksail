//go:build ignore

// Run with an already built KSail binary. All fixtures and requests are local;
// the credential command is a tripwire that must never execute.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: local-connection-recovery-trial <ksail-binary>")
		os.Exit(1)
	}
	if err := trial(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func trial(binary string) error {
	dir, err := os.MkdirTemp("", "ksail-local-recovery-trial-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()
	marker := filepath.Join(dir, "credential-plugin-ran")
	path := filepath.Join(dir, "config")
	config := clientcmdapi.NewConfig()
	config.Clusters["shared"] = &clientcmdapi.Cluster{Server: server.URL}
	config.AuthInfos["shared"] = &clientcmdapi.AuthInfo{Exec: &clientcmdapi.ExecConfig{
		APIVersion: "client.authentication.k8s.io/v1", Command: "touch", Args: []string{marker},
		InteractiveMode: clientcmdapi.NeverExecInteractiveMode,
	}}
	config.Contexts["kind-nested"] = &clientcmdapi.Context{Cluster: "shared", AuthInfo: "shared"}
	config.Contexts["host"] = &clientcmdapi.Context{Cluster: "shared", AuthInfo: "shared", Namespace: "preserved"}
	config.CurrentContext = "kind-nested"
	if err := clientcmd.WriteToFile(*config, path); err != nil {
		return err
	}
	before, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	run := func(args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, append([]string{"cluster", "forget"}, args...)...)
		command.Dir = dir
		command.Env = append(os.Environ(), "KUBECONFIG="+path, "OMNI_ENDPOINT=", "OMNI_SERVICE_ACCOUNT_KEY=")
		output, runErr := command.CombinedOutput()
		fmt.Print(string(output))
		return runErr
	}
	if err := run("--kubeconfig", path, "--context", "kind-nested"); err == nil {
		return errors.New("default-off invocation unexpectedly succeeded")
	}
	if err := unchanged(path, before); err != nil {
		return err
	}
	if err := run("--experimental", "--context", "kind-nested"); err == nil {
		return errors.New("missing explicit file unexpectedly succeeded")
	}
	if err := unchanged(path, before); err != nil {
		return err
	}
	if err := run("--experimental", "--kubeconfig", path, "--context", "kind-nested"); err != nil {
		return fmt.Errorf("recover local context: %w", err)
	}
	actual, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return err
	}
	if _, exists := actual.Contexts["kind-nested"]; exists {
		return errors.New("selected context remains")
	}
	if len(actual.Contexts) != 1 || actual.Contexts["host"] == nil || actual.Contexts["host"].Namespace != "preserved" ||
		actual.Clusters["shared"] == nil || actual.AuthInfos["shared"] == nil || actual.CurrentContext != "" {
		return errors.New("shared host connection was changed")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := run("--experimental", "--kubeconfig", path, "--context", "kind-nested"); err != nil {
		return fmt.Errorf("idempotent retry: %w", err)
	}
	if err := unchanged(path, after); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o400); err != nil {
		return err
	}
	if err := run("--experimental", "--kubeconfig", path, "--context", "host"); err == nil {
		return errors.New("read-only kubeconfig was replaced")
	}
	if err := unchanged(path, after); err != nil {
		return err
	}
	if requests.Load() != 0 {
		return fmt.Errorf("local recovery contacted the recording API %d times", requests.Load())
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("credential execution tripwire was not absent: %v", err)
	}
	fmt.Println("PASS: actual CLI default-off, explicit selection, shared preservation, retry and read-only refusal; API requests=0, credential executions=0")
	return nil
}

func unchanged(path string, expected []byte) error {
	actual, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, expected) {
		return errors.New("kubeconfig changed on a refused or idempotent invocation")
	}
	return nil
}
