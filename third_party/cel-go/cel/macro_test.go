// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cel

import (
	"testing"

	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/ast"
)

func TestGlobalVarArgMacro(t *testing.T) {
	noopExpander := func(meh MacroExprFactory, target ast.Expr, args []ast.Expr) (ast.Expr, *common.Error) {
		return nil, nil
	}
	varArgMacro := GlobalVarArgMacro("varargs", noopExpander)
	if varArgMacro.ArgCount() != 0 {
		t.Errorf("ArgCount() got %d, wanted 0", varArgMacro.ArgCount())
	}
	if varArgMacro.Function() != "varargs" {
		t.Errorf("Function() got %q, wanted 'varargs'", varArgMacro.Function())
	}
	if varArgMacro.MacroKey() != "varargs:*:false" {
		t.Errorf("MacroKey() got %q, wanted 'varargs:*:false'", varArgMacro.MacroKey())
	}
	if varArgMacro.IsReceiverStyle() {
		t.Errorf("IsReceiverStyle() got %t, wanted false", varArgMacro.IsReceiverStyle())
	}
}

func TestReceiverVarArgMacro(t *testing.T) {
	noopExpander := func(meh MacroExprFactory, target ast.Expr, args []ast.Expr) (ast.Expr, *common.Error) {
		return nil, nil
	}
	varArgMacro := ReceiverVarArgMacro("varargs", noopExpander)
	if varArgMacro.ArgCount() != 0 {
		t.Errorf("ArgCount() got %d, wanted 0", varArgMacro.ArgCount())
	}
	if varArgMacro.Function() != "varargs" {
		t.Errorf("Function() got %q, wanted 'varargs'", varArgMacro.Function())
	}
	if varArgMacro.MacroKey() != "varargs:*:true" {
		t.Errorf("MacroKey() got %q, wanted 'varargs:*:true'", varArgMacro.MacroKey())
	}
	if !varArgMacro.IsReceiverStyle() {
		t.Errorf("IsReceiverStyle() got %t, wanted true", varArgMacro.IsReceiverStyle())
	}
}

func TestDocumentation(t *testing.T) {
	noopExpander := func(meh MacroExprFactory, target ast.Expr, args []ast.Expr) (ast.Expr, *common.Error) {
		return nil, nil
	}
	varArgMacro := ReceiverVarArgMacro("varargs", noopExpander,
		MacroDocs(`convert variable argument lists to a list literal`),
		MacroExamples(`fn.varargs(1,2,3) // fn([1, 2, 3])`))
	doc, ok := varArgMacro.(common.Documentor)
	if !ok {
		t.Fatal("macro does not implement Documenter interface")
	}
	d := doc.Documentation()
	if d.Kind != common.DocMacro {
		t.Errorf("Documentation() got kind %v, wanted DocMacro", d.Kind)
	}
	if d.Name != varArgMacro.Function() {
		t.Errorf("Documentation() got name %q, wanted %q", d.Name, varArgMacro.Function())
	}
	if d.Description != `convert variable argument lists to a list literal` {
		t.Errorf("Documentation() got description %q, wanted %q", d.Description, `convert variable argument lists to a list literal`)
	}
	if len(d.Children) != 1 {
		t.Fatalf("macro documentation children got: %d", len(d.Children))
	}
	if d.Children[0].Description != `fn.varargs(1,2,3) // fn([1, 2, 3])` {
		t.Errorf("macro documentation Children[0] got %s, wanted %s", d.Children[0].Description,
			`fn.varargs(1,2,3) // fn([1, 2, 3])`)
	}
}
