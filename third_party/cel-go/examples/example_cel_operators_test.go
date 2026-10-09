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

// Example_cel_Arithmetic showcases negation, basic operations, modulo, and precedence
// as documented in https://celbyexample.com/arithmetic/
func Example_cel_Arithmetic() {
	exprs := []string{
		`-5`,
		`10 + 20`,
		`30 - 12`,
		`6 * 7`,
		`20 / 4`,
		`7 % 3`,
		`2 + 3 * 4`,
		`(2 + 3) * 4`,
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
	// -5 -> -5
	// 10 + 20 -> 30
	// 30 - 12 -> 18
	// 6 * 7 -> 42
	// 20 / 4 -> 5
	// 7 % 3 -> 1
	// 2 + 3 * 4 -> 14
	// (2 + 3) * 4 -> 20
}

// Example_cel_Comparison showcases equality and ordering comparison operators
// as documented in https://celbyexample.com/comparison/
func Example_cel_Comparison() {
	exprs := []string{
		`10 == 10`,
		`10 != 20`,
		`"hello" == "hello"`,
		`"apple" < "banana"`,
		`5 < 10`,
		`10 <= 10`,
		`15 > 10`,
		`10 >= 10`,
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
	// 10 == 10 -> true
	// 10 != 20 -> true
	// "hello" == "hello" -> true
	// "apple" < "banana" -> true
	// 5 < 10 -> true
	// 10 <= 10 -> true
	// 15 > 10 -> true
	// 10 >= 10 -> true
}

// Example_cel_Null showcases null literal and null equality checks
// as documented in https://celbyexample.com/null/
func Example_cel_Null() {
	exprs := []string{
		`null`,
		`val == null`,
		`val != null`,
	}

	for _, expr := range exprs {
		prg, err := cel.Compile(expr, cel.Variable("val", cel.DynType))
		if err != nil {
			log.Fatalf("cel.Compile() error for %q: %v", expr, err)
		}
		out, _, err := prg.Eval(map[string]any{"val": nil})
		if err != nil {
			log.Fatalf("prg.Eval() error for %q: %v", expr, err)
		}
		fmt.Printf("%s -> %v\n", expr, out)
	}

	// Output:
	// null -> 0
	// val == null -> true
	// val != null -> false
}
