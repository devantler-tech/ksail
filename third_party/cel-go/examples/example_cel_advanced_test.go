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

	"github.com/google/cel-go/cel"
)

// Example_cel_CommonErrors showcases handling common runtime errors (division by zero, index out of bounds, missing key)
// as documented in https://celbyexample.com/common-errors/
func Example_cel_CommonErrors() {
	vars := map[string]any{
		"m": map[string]int64{"a": 1},
		"l": []int64{10, 20},
	}

	exprs := []string{
		`1 / 0`,
		`l[5]`,
		`m["missing"]`,
	}

	for _, expr := range exprs {
		prg, err := cel.Compile(expr,
			cel.Variable("m", cel.MapType(cel.StringType, cel.IntType)),
			cel.Variable("l", cel.ListType(cel.IntType)),
		)
		if err != nil {
			fmt.Printf("%s -> compile error: %v\n", expr, err)
			continue
		}
		_, _, err = prg.Eval(vars)
		if err != nil {
			fmt.Printf("%s -> runtime error: %v\n", expr, err)
		}
	}

	// Output:
	// 1 / 0 -> runtime error: division by zero
	// l[5] -> runtime error: index out of bounds: 5
	// m["missing"] -> runtime error: no such key: missing
}

// Example_cel_NameResolution showcases scope resolution order and macro variable shadowing
// as documented in https://celbyexample.com/name-resolution/
func Example_cel_NameResolution() {
	vars := map[string]any{
		"x":     100,
		"items": []int64{1, 2, 3},
	}

	// In items.map(x, x * 2), the iteration variable 'x' shadows the outer variable 'x'
	prg, err := cel.Compile(`items.map(x, x * 2)`,
		cel.Variable("x", cel.IntType),
		cel.Variable("items", cel.ListType(cel.IntType)),
	)
	if err != nil {
		fmt.Printf("cel.Compile() error: %v\n", err)
		return
	}
	out, _, err := prg.Eval(vars)
	if err != nil {
		fmt.Printf("prg.Eval() error: %v\n", err)
		return
	}

	fmt.Printf("Macro iteration variable shadows outer x: %v\n", out)

	// Output:
	// Macro iteration variable shadows outer x: [2, 4, 6]
}
