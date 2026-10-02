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
	"github.com/google/cel-go/ext"
)

// Example_cel_Compile showcases compiling a CEL expression with variable declarations
func Example_cel_Compile() {
	prg, err := cel.Compile(`"Hello world! I'm " + name + "."`,
		cel.Variable("name", cel.StringType),
	)
	if err != nil {
		log.Fatalf("cel.Compile() failed: %v", err)
	}
	out, _, err := prg.Eval(map[string]any{
		"name": "CEL",
	})
	if err != nil {
		log.Fatalf("prg.Eval() failed: %v", err)
	}
	fmt.Println(out)

	// Output:
	// Hello world! I'm CEL.
}

// Example_cel_Compile_options showcases cel.Compile with extension options and multiple variables
func Example_cel_Compile_options() {
	prg, err := cel.Compile(`"%s! I'm %s.".format([greeting, name])`,
		ext.Strings(),
		cel.Variable("greeting", cel.StringType),
		cel.Variable("name", cel.StringType),
	)
	if err != nil {
		log.Fatalf("cel.Compile() failed: %v", err)
	}
	out, _, err := prg.Eval(map[string]any{
		"greeting": "Hello world",
		"name":     "CEL",
	})
	if err != nil {
		log.Fatalf("prg.Eval() failed: %v", err)
	}
	fmt.Println(out)

	// Output:
	// Hello world! I'm CEL.
}
