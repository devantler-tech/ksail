package scaffolder

import (
	"encoding/json"
	"strings"
	"testing"

	yamlgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/yaml"
)

// TestDevcontainerGeneratorReturnsContentWhenNoOutput pins the generator's
// content-only contract: with an empty Output it returns the rendered definition
// instead of writing a file (mirroring schemaHeaderGenerator).
func TestDevcontainerGeneratorReturnsContentWhenNoOutput(t *testing.T) {
	t.Parallel()

	const want = "devcontainer-content"

	gen := &devcontainerGenerator{content: want}

	got, err := gen.Generate(struct{}{}, yamlgenerator.Options{Output: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != want {
		t.Fatalf("content-only Generate = %q, want %q", got, want)
	}
}

// TestDevcontainerNameDefault confirms the cosmetic name falls back to the
// default when no cluster name override is set.
func TestDevcontainerNameDefault(t *testing.T) {
	t.Parallel()

	defaultName := (&Scaffolder{}).devcontainerName()
	if defaultName != defaultDevcontainerName {
		t.Fatalf("default devcontainerName = %q, want %q", defaultName, defaultDevcontainerName)
	}

	named := (&Scaffolder{ClusterName: "prod"}).devcontainerName()
	if named != "prod" {
		t.Fatalf("override devcontainerName = %q, want %q", named, "prod")
	}
}

// TestDevcontainerTemplateRendersValidJSON guards the raw template: substituting
// a JSON-quoted name must yield parseable JSON carrying the Docker-in-Docker
// feature and the KSail install step.
func TestDevcontainerTemplateRendersValidJSON(t *testing.T) {
	t.Parallel()

	content := strings.Replace(devcontainerJSONTemplate, "%s", `"demo"`, 1)
	if !json.Valid([]byte(content)) {
		t.Fatalf("rendered devcontainer template is not valid JSON:\n%s", content)
	}

	for _, want := range []string{"docker-in-docker", "go install github.com/devantler-tech/ksail"} {
		if !strings.Contains(content, want) {
			t.Fatalf("rendered template missing %q", want)
		}
	}
}
