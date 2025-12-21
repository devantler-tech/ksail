package docker_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
)

func TestGetDockerClient(t *testing.T) {
	t.Parallel()

	client, err := docker.GetDockerClient()
	if err != nil {
		if client != nil {
			t.Fatalf("expected nil client on error, got %v", client)
		}

		return
	}

	if client == nil {
		t.Fatalf("expected client when no error returned")
	}
}

func TestGetDockerClient_InvalidEnv(t *testing.T) {
	t.Setenv("DOCKER_HOST", "://")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	client, err := docker.GetDockerClient()
	if err == nil {
		t.Fatal("expected error for malformed DOCKER_HOST")
	}

	if client != nil {
		t.Fatalf("expected nil client when creation fails, got %v", client)
	}
}
