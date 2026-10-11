// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ext

import (
	"fmt"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/env"
)

func TestExtensionOptionFactoryInvalidExtension(t *testing.T) {
	invalidExtension := "invalid extension"
	_, validExtension := ExtensionOptionFactory(invalidExtension)
	if validExtension {
		t.Fatalf("ExtensionOptionFactory(%s) returned valid extension for invalid input", invalidExtension)
	}
}

func TestExtensionOptionFactoryInvalidExtensionName(t *testing.T) {
	e := &env.Extension{Name: "invalid extension name"}
	_, validExtension := ExtensionOptionFactory(e)
	if validExtension {
		t.Fatalf("ExtensionOptionFactory(%s) returned valid extension for invalid extension name", e.Name)
	}
}

func TestExtensionOptionFactoryInvalidExtensionVersion(t *testing.T) {
	e := &env.Extension{Name: "bindings", Version: "invalid version"}
	opt, validExtension := ExtensionOptionFactory(e)
	if !validExtension {
		t.Fatalf("ExtensionOptionFactory(%s) returned invalid extension", e.Name)
	}
	_, err := cel.NewCustomEnv(opt)
	if err == nil || err.Error() != fmt.Sprintf("invalid extension version: %s - %s", e.Name, e.Version) {
		t.Fatalf("ExtensionOptionFactory(%s) returned invalid extension version", e.Name)
	}
}

func TestExtensionOptionFactoryValidBindingsExtension(t *testing.T) {
	e := &env.Extension{Name: "cel.lib.ext.cel.bindings", Version: "latest"}
	opt, validExtension := ExtensionOptionFactory(e)
	if !validExtension {
		t.Fatalf("ExtensionOptionFactory(%s) returned invalid extension", e.Name)
	}
	en, err := cel.NewCustomEnv(opt)
	if err != nil {
		t.Fatalf("ExtensionOptionFactory(%s) returned invalid extension", e.Name)
	}
	cfg, err := en.ToConfig("test config")
	if err != nil {
		t.Fatalf("ToConfig(%s) returned error: %v", e.Name, err)
	}
	if len(cfg.Extensions) != 1 || cfg.Extensions[0].Name != "cel.lib.ext.cel.bindings" || cfg.Extensions[0].Version != "latest" {
		t.Fatalf("ExtensionOptionFactory(%s) returned invalid extension", e.Name)
	}
}

func TestExtensionOptionFactoryValidBindingsExtensionAlias(t *testing.T) {
	e := &env.Extension{Name: "bindings", Version: "latest"}
	opt, validExtension := ExtensionOptionFactory(e)
	if !validExtension {
		t.Fatalf("ExtensionOptionFactory(%s) returned invalid extension", e.Name)
	}
	en, err := cel.NewCustomEnv(opt)
	if err != nil {
		t.Fatalf("ExtensionOptionFactory(%s) returned invalid extension", e.Name)
	}
	cfg, err := en.ToConfig("test config")
	if err != nil {
		t.Fatalf("ToConfig(%s) returned error: %v", e.Name, err)
	}
	if len(cfg.Extensions) != 1 || cfg.Extensions[0].Name != "cel.lib.ext.cel.bindings" || cfg.Extensions[0].Version != "latest" {
		t.Fatalf("ExtensionOptionFactory(%s) returned invalid extension", e.Name)
	}
}
