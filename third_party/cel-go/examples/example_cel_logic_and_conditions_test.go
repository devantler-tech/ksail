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
)

// Example_cel_LogicAndConditions showcases logical operators, conditional (ternary) operator, and evaluation
// on user data as documented in https://celbyexample.com/logic-and-conditions/
func Example_cel_LogicAndConditions() {
	userData := map[string]any{
		"is_active":  true,
		"is_admin":   false,
		"age":        25,
		"risk_score": 0.2,
	}

	exprs := []string{
		`user.is_active && user.age >= 18`,
		`user.is_admin || user.risk_score < 0.5`,
		`!user.is_admin`,
		`user.is_admin ? "full_access" : (user.is_active ? "standard_access" : "no_access")`,
	}

	for _, expr := range exprs {
		prg, err := cel.Compile(expr, cel.Variable("user", cel.DynType))
		if err != nil {
			log.Fatalf("cel.Compile() error for %q: %v", expr, err)
		}
		out, _, err := prg.Eval(map[string]any{"user": userData})
		if err != nil {
			log.Fatalf("prg.Eval() error for %q: %v", expr, err)
		}
		fmt.Printf("%s -> %v\n", expr, out)
	}

	// Output:
	// user.is_active && user.age >= 18 -> true
	// user.is_admin || user.risk_score < 0.5 -> true
	// !user.is_admin -> true
	// user.is_admin ? "full_access" : (user.is_active ? "standard_access" : "no_access") -> standard_access
}

// Example_cel_LogicalOperators showcases logical AND, OR, NOT, and short-circuiting
// as documented in https://celbyexample.com/logical-operators/
func Example_cel_LogicalOperators() {
	exprs := []string{
		`true && false`,
		`true || false`,
		`!true`,
		`false && (1 / 0 == 0)`,
		`true || (1 / 0 == 0)`,
		`!false`,
	}

	for _, expr := range exprs {
		prg, err := cel.Compile(expr)
		if err != nil {
			log.Fatalf("cel.Compile() error for %q: %v", expr, err)
		}
		out, _, err := prg.Eval(cel.NoVars())
		if err != nil {
			log.Fatalf("prg.Eval() error for %q: %v", expr, err)
		}
		fmt.Printf("%s -> %v\n", expr, out)
	}

	// Output:
	// true && false -> false
	// true || false -> true
	// !true -> false
	// false && (1 / 0 == 0) -> false
	// true || (1 / 0 == 0) -> true
	// !false -> true
}

// Example_cel_Ternary showcases ternary conditional expressions
// as documented in https://celbyexample.com/ternary/
func Example_cel_Ternary() {
	exprs := []string{
		`true ? "yes" : "no"`,
		`false ? 1 : 2`,
		`10 > 5 ? "greater" : "lesser"`,
	}

	for _, expr := range exprs {
		prg, err := cel.Compile(expr)
		if err != nil {
			log.Fatalf("cel.Compile() error for %q: %v", expr, err)
		}
		out, _, err := prg.Eval(cel.NoVars())
		if err != nil {
			log.Fatalf("prg.Eval() error for %q: %v", expr, err)
		}
		fmt.Printf("%s -> %v\n", expr, out)
	}

	// Output:
	// true ? "yes" : "no" -> yes
	// false ? 1 : 2 -> 2
	// 10 > 5 ? "greater" : "lesser" -> greater
}
