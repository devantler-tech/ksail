// Copyright 2020 Google LLC
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
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Example_cel_Overload showcases defining custom global functions with cel.Overload
// as documented in https://celbyexample.com/custom-functions/
func Example_cel_Overload() {
	prg, err := cel.Compile(`shake_hands(i,you)`,
		cel.Variable("i", cel.StringType),
		cel.Variable("you", cel.StringType),
		cel.Function("shake_hands",
			cel.Overload("shake_hands_string_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.StringType,
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
					return types.String(
						fmt.Sprintf("%s and %s are shaking hands.", lhs, rhs))
				}),
			),
		),
	)
	if err != nil {
		log.Fatalf("cel.Compile() error: %v", err)
	}

	out, _, err := prg.Eval(map[string]any{
		"i":   "CEL",
		"you": "world",
	})
	if err != nil {
		log.Fatalf("prg.Eval() error: %v", err)
	}

	fmt.Println(out)
	// Output:
	// CEL and world are shaking hands.
}

// Example_cel_MemberOverload showcases defining custom receiver/member functions with cel.MemberOverload
// as documented in https://celbyexample.com/custom-functions/
func Example_cel_MemberOverload() {
	prg, err := cel.Compile(`i.greet(you)`,
		cel.Lib(customGreetLib{}),
	)
	if err != nil {
		log.Fatalf("cel.Compile() error: %v", err)
	}

	out, _, err := prg.Eval(map[string]any{
		"i":   "CEL",
		"you": "world",
	})
	if err != nil {
		log.Fatalf("prg.Eval() error: %v", err)
	}

	fmt.Println(out)
	// Output:
	// Hello world! Nice to meet you, I'm CEL.
}

type customGreetLib struct{}

func (customGreetLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Variable("i", cel.StringType),
		cel.Variable("you", cel.StringType),
		cel.Function("greet",
			cel.MemberOverload("string_greet_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.StringType,
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
					return types.String(
						fmt.Sprintf("Hello %s! Nice to meet you, I'm %s.", rhs, lhs))
				}),
			),
		),
	}
}

func (customGreetLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{}
}

// Example_cel_CustomFunctions showcases type safety and receiver syntax for custom functions
func Example_cel_CustomFunctions() {
	prg, err := cel.Compile(`name.greet()`,
		cel.Variable("name", cel.StringType),
		cel.Function("greet",
			cel.MemberOverload("string_greet",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					return types.String("Hello, " + string(val.(types.String)))
				}),
			),
		),
	)
	if err != nil {
		log.Fatalf("cel.Compile() error: %v", err)
	}

	out, _, err := prg.Eval(map[string]any{
		"name": "Alice",
	})
	if err != nil {
		log.Fatalf("prg.Eval() error: %v", err)
	}

	fmt.Println(out)
	// Output:
	// Hello, Alice
}
