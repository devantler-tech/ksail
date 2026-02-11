package parser_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/image/parser"
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
