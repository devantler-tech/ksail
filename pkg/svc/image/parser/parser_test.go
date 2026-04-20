package parser_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/image/parser"
)

const (
	kindNodePattern   = `FROM\s+(kindest/node:[^\s]+)`
	kindNodeImageName = "Kind node"
	k3sPattern        = `FROM\s+(rancher/k3s:[^\s]+)`
	k3sImageName      = "K3s"
)

func TestParseImageFromDockerfile_Success(t *testing.T) {
	t.Parallel()

	dockerfile := "FROM kindest/node:v1.32.2"

	result := parser.ParseImageFromDockerfile(dockerfile, kindNodePattern, kindNodeImageName)
	expected := "kindest/node:v1.32.2"

	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestParseImageFromDockerfile_WithMultipleSpaces(t *testing.T) {
	t.Parallel()

	dockerfile := "FROM    rancher/k3s:v1.32.2-k3s1"

	result := parser.ParseImageFromDockerfile(dockerfile, k3sPattern, k3sImageName)
	expected := "rancher/k3s:v1.32.2-k3s1"

	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestParseImageFromDockerfile_InvalidDockerfile(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic but got none")
		}
	}()

	dockerfile := "INVALID CONTENT"

	parser.ParseImageFromDockerfile(dockerfile, kindNodePattern, kindNodeImageName)
}

func TestParseImageFromDockerfile_EmptyDockerfile(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic but got none")
		}
	}()

	dockerfile := ""

	parser.ParseImageFromDockerfile(dockerfile, kindNodePattern, kindNodeImageName)
}

func TestParseAllImagesFromDockerfile_MultipleImages(t *testing.T) {
	t.Parallel()

	dockerfile := `# Comment line
FROM ghcr.io/fluxcd/source-controller:v1.8.1
FROM ghcr.io/fluxcd/kustomize-controller:v1.8.1
FROM ghcr.io/fluxcd/helm-controller:v1.5.1
FROM ghcr.io/fluxcd/notification-controller:v1.8.1`

	result := parser.ParseAllImagesFromDockerfile(dockerfile)

	expected := []string{
		"ghcr.io/fluxcd/source-controller:v1.8.1",
		"ghcr.io/fluxcd/kustomize-controller:v1.8.1",
		"ghcr.io/fluxcd/helm-controller:v1.5.1",
		"ghcr.io/fluxcd/notification-controller:v1.8.1",
	}

	if len(result) != len(expected) {
		t.Fatalf("Expected %d images, got %d", len(expected), len(result))
	}

	for i, img := range expected {
		if result[i] != img {
			t.Errorf("Image %d: expected %s, got %s", i, img, result[i])
		}
	}
}

func TestParseAllImagesFromDockerfile_SingleImage(t *testing.T) {
	t.Parallel()

	dockerfile := "FROM kindest/node:v1.32.2"

	result := parser.ParseAllImagesFromDockerfile(dockerfile)

	if len(result) != 1 {
		t.Fatalf("Expected 1 image, got %d", len(result))
	}

	if result[0] != "kindest/node:v1.32.2" {
		t.Errorf("Expected kindest/node:v1.32.2, got %s", result[0])
	}
}

func TestParseAllImagesFromDockerfile_EmptyDockerfile(t *testing.T) {
	t.Parallel()

	result := parser.ParseAllImagesFromDockerfile("")

	if len(result) != 0 {
		t.Errorf("Expected 0 images, got %d", len(result))
	}
}

func TestParseAllImagesFromDockerfile_NoFromDirectives(t *testing.T) {
	t.Parallel()

	dockerfile := "# Just a comment\nRUN echo hello"

	result := parser.ParseAllImagesFromDockerfile(dockerfile)

	if len(result) != 0 {
		t.Errorf("Expected 0 images, got %d", len(result))
	}
}

func TestParseAllImagesFromDockerfile_WithPlatformFlag(t *testing.T) {
	t.Parallel()

	dockerfile := "FROM --platform=linux/amd64 ghcr.io/fluxcd/source-controller:v1.8.1"

	result := parser.ParseAllImagesFromDockerfile(dockerfile)

	if len(result) != 1 {
		t.Fatalf("Expected 1 image, got %d", len(result))
	}

	expected := "ghcr.io/fluxcd/source-controller:v1.8.1"
	if result[0] != expected {
		t.Errorf("Expected %s, got %s", expected, result[0])
	}
}
