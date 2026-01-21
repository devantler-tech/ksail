package imageparser

import (
	"testing"
)

func TestParseImageFromDockerfile_Success(t *testing.T) {
	dockerfile := "FROM kindest/node:v1.32.2"
	pattern := `FROM\s+(kindest/node:[^\s]+)`
	imageName := "Kind node"

	result := ParseImageFromDockerfile(dockerfile, pattern, imageName)
	expected := "kindest/node:v1.32.2"

	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestParseImageFromDockerfile_WithMultipleSpaces(t *testing.T) {
	dockerfile := "FROM    rancher/k3s:v1.32.2-k3s1"
	pattern := `FROM\s+(rancher/k3s:[^\s]+)`
	imageName := "K3s"

	result := ParseImageFromDockerfile(dockerfile, pattern, imageName)
	expected := "rancher/k3s:v1.32.2-k3s1"

	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestParseImageFromDockerfile_InvalidDockerfile(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic but got none")
		}
	}()

	dockerfile := "INVALID CONTENT"
	pattern := `FROM\s+(kindest/node:[^\s]+)`
	imageName := "Kind node"

	ParseImageFromDockerfile(dockerfile, pattern, imageName)
}

func TestParseImageFromDockerfile_EmptyDockerfile(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic but got none")
		}
	}()

	dockerfile := ""
	pattern := `FROM\s+(kindest/node:[^\s]+)`
	imageName := "Kind node"

	ParseImageFromDockerfile(dockerfile, pattern, imageName)
}
