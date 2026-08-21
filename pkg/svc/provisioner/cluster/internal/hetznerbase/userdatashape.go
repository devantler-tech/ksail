package hetznerbase

import (
	"errors"
	"fmt"

	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	containerdbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/containerd"
	kubeadmbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/kubeadm"
	"gopkg.in/yaml.v3"
)

// ErrUserDataShapeNotAllowed refuses user-data whose STRUCTURE is outside what
// the bootstrap renderers emit.
//
// This is the allowlist half of the provider-boundary guard, and it is what the
// denylist beside it cannot be: complete. The denylist matches spellings of
// dangerous content, and shell can spell one flag or one path in unboundedly
// many ways, so each newly-found spelling buys only "no known bypass". The set
// of documents ksail itself emits is small and enumerable, so constraining the
// output to that set refuses everything outside it regardless of spelling.
var ErrUserDataShapeNotAllowed = errors.New(
	"hetzner: provider user-data has a shape the bootstrap renderers never emit",
)

// isAllowedTopLevelKey reports whether key is one of the cloud-config modules the
// renderers emit, taken from the cloudConfig struct in the cloud-init bootstrap
// package. Anything else -- notably `bootcmd`, which runs commands earlier than
// `runcmd` -- is refused, because no bring-up produces it.
func isAllowedTopLevelKey(key string) bool {
	switch key {
	case "write_files", "apt", "packages", "ssh_authorized_keys", "ssh_keys", "runcmd":
		return true
	default:
		return false
	}
}

// isAllowedWriteTarget reports whether path is one the renderers write. The
// generated boot script is the cloud-init transport's own; the other two come
// from the kubeadm and containerd bootstrappers. Referencing their exported
// constants rather than restating the strings means a renderer that starts
// writing somewhere new trips this guard's own test instead of drifting past it.
func isAllowedWriteTarget(path string) bool {
	switch path {
	case cloudinitbootstrap.DefaultScriptPath,
		containerdbootstrap.ConfigPath,
		kubeadmbootstrap.ConfigPath:
		return true
	default:
		return false
	}
}

// validateUserDataShape refuses any document whose top-level modules, write_files
// targets, or runcmd entries fall outside what the renderers emit.
//
// It runs AFTER the PEM and signing-transport guards so their more specific
// refusals keep reporting first: an operator seeing signing material in
// user-data is better served by "we found signing material" than by "this shape
// is not allowed", even though both are true.
//
// An empty or comment-only document carries no module and is accepted: it
// delivers nothing, and the provisioners emit exactly that for a node with no
// directives.
func validateUserDataShape(userData string) error {
	var detail string

	err := scanDocuments(userData, ErrUserDataShapeNotAllowed, func(document *yaml.Node) bool {
		detail = disallowedShape(document)

		return detail != ""
	})
	if err != nil {
		if errors.Is(err, ErrUserDataShapeNotAllowed) {
			return fmt.Errorf("%w: %s", ErrUserDataShapeNotAllowed, detail)
		}

		return err
	}

	return nil
}

// disallowedShape returns a description of the first structural violation in
// one document, or "" when the document is within the allowlist.
func disallowedShape(document *yaml.Node) string {
	mapping := documentMapping(document)
	if mapping == nil {
		return ""
	}

	for index := 0; index+1 < len(mapping.Content); index += 2 {
		key, value := mapping.Content[index], mapping.Content[index+1]

		if key.Kind != yaml.ScalarNode {
			return "a non-scalar top-level key"
		}

		if !isAllowedTopLevelKey(key.Value) {
			return fmt.Sprintf("top-level key %q", key.Value)
		}

		if detail := disallowedModule(key.Value, value); detail != "" {
			return detail
		}
	}

	return ""
}

// disallowedModule applies the per-module rules for the two modules that carry a
// target rather than only data.
func disallowedModule(key string, value *yaml.Node) string {
	switch key {
	case "write_files":
		return disallowedWriteFiles(value)
	case "runcmd":
		return disallowedRunCmd(value)
	default:
		return ""
	}
}

// documentMapping returns the mapping a document node wraps, or nil when the
// document carries no mapping (an empty or comment-only document).
//
// An alias is deliberately NOT followed: a top-level alias is not something the
// renderers emit, and resolving one here would reintroduce the cyclic-expansion
// hazard the scalar walk guards against.
func documentMapping(node *yaml.Node) *yaml.Node {
	current := node
	if current.Kind == yaml.DocumentNode {
		if len(current.Content) == 0 {
			return nil
		}

		current = current.Content[0]
	}

	if current.Kind != yaml.MappingNode {
		return nil
	}

	return current
}

// disallowedWriteFiles refuses any entry whose path is not one the renderers
// write. The path is read as a plain scalar, so no spelling of the VALUE can
// change which target it names.
func disallowedWriteFiles(value *yaml.Node) string {
	if value.Kind != yaml.SequenceNode {
		return "a write_files value that is not a list"
	}

	for _, entry := range value.Content {
		if entry.Kind != yaml.MappingNode {
			return "a write_files entry that is not a mapping"
		}

		path, found := scalarField(entry, "path")
		if !found {
			return "a write_files entry with no scalar path"
		}

		if !isAllowedWriteTarget(path) {
			return fmt.Sprintf("write_files target %q", path)
		}
	}

	return ""
}

// disallowedRunCmd pins runcmd to the single argv the renderers emit:
// `/bin/sh <generated boot script>`. cloud-init also accepts a bare string,
// which it runs through a shell; the renderers never emit that form, so it is
// refused rather than parsed.
func disallowedRunCmd(value *yaml.Node) string {
	if value.Kind != yaml.SequenceNode {
		return "a runcmd value that is not a list"
	}

	for _, entry := range value.Content {
		if entry.Kind != yaml.SequenceNode || len(entry.Content) != 2 {
			return "a runcmd entry that is not a two-element argv"
		}

		shell, script := entry.Content[0], entry.Content[1]
		if shell.Kind != yaml.ScalarNode || script.Kind != yaml.ScalarNode {
			return "a runcmd entry with a non-scalar element"
		}

		if shell.Value != runCmdShell {
			return fmt.Sprintf("runcmd interpreter %q", shell.Value)
		}

		if !isAllowedWriteTarget(script.Value) {
			return fmt.Sprintf("runcmd script %q", script.Value)
		}
	}

	return ""
}

// runCmdShell is the interpreter the cloud-init renderer invokes the generated
// boot script with.
const runCmdShell = "/bin/sh"

// scalarField returns the scalar value of mapping's key, and whether one was
// found. A non-scalar value reports not-found, so a structured value cannot pass
// as a path.
func scalarField(mapping *yaml.Node, name string) (string, bool) {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		key, value := mapping.Content[index], mapping.Content[index+1]
		if key.Kind == yaml.ScalarNode && key.Value == name {
			if value.Kind != yaml.ScalarNode {
				return "", false
			}

			return value.Value, true
		}
	}

	return "", false
}
