// Copyright 2026 Google LLC
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

package examples

import (
	"fmt"
	"log"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/parser"
)

// Example_cel_CustomMacros showcases defining custom AST transformation macros
// as documented in https://celbyexample.com/custom-macros/
func Example_cel_CustomMacros() {
	// Define a custom macro "join_str(a, b)" that expands at compile time to "a + ' ' + b"
	joinMacro := parser.NewGlobalMacro("join_str", 2, func(eh parser.ExprHelper, target ast.Expr, args []ast.Expr) (ast.Expr, *common.Error) {
		space := eh.NewLiteral(types.String(" "))
		firstConcat := eh.NewCall(operators.Add, args[0], space)
		return eh.NewCall(operators.Add, firstConcat, args[1]), nil
	})

	prg, err := cel.Compile(`join_str("Hello", "World")`, cel.Macros(joinMacro))
	if err != nil {
		log.Fatalf("cel.Compile() error: %v", err)
	}

	out, _, err := prg.Eval(cel.NoVars())
	if err != nil {
		log.Fatalf("prg.Eval() error: %v", err)
	}

	fmt.Println(out)
	// Output:
	// Hello World
}
