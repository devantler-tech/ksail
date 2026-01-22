package imageparser_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/io/imageparser"
)

const (
	kindNodePattern   = `FROM\s+(kindest/node:[^\s]+)`
	kindNodeImageName = "Kind node"
)

func TestParseImageFromDockerfile_Success(t *testing.T) {
	t.Parallel()

	dockerfile := "FROM kindest/node:v1.32.2"

	result := imageparser.ParseImageFromDockerfile(dockerfile, kindNodePattern, kindNodeImageName)
	expected := "kindest/node:v1.32.2"

	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestParseImageFromDockerfile_WithMultipleSpaces(t *testing.T) {
	t.Parallel()

	dockerfile := "FROM    rancher/k3s:v1.32.2-k3s1"
	pattern := `FROM\s+(rancher/k3s:[^\s]+)`
	imageName := "K3s"

	result := imageparser.ParseImageFromDockerfile(dockerfile, pattern, imageName)
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

	imageparser.ParseImageFromDockerfile(dockerfile, kindNodePattern, kindNodeImageName)
}

func TestParseImageFromDockerfile_EmptyDockerfile(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic but got none")
		}
	}()

	dockerfile := ""

	imageparser.ParseImageFromDockerfile(dockerfile, kindNodePattern, kindNodeImageName)
}
