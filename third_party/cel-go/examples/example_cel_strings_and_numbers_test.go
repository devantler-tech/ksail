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
	"github.com/google/cel-go/ext"
)

// Example_cel_StringsAndNumbers showcases string functions, concatenation, and numeric comparisons
// on user data as documented in https://celbyexample.com/strings-and-numbers/
func Example_cel_StringsAndNumbers() {
	userData := map[string]any{
		"name":  "Alice",
		"roles": []string{"admin", "editor", "viewer"},
		"age":   30,
		"score": 95.5,
	}

	exprs := []string{
		`user.name.endsWith("ice")`,
		`"admin" in user.roles`,
		`user.age >= 18`,
		`user.score > 90.0`,
		`user.name + " (" + string(user.age) + ")"`,
		`size(user.roles)`,
		`user.name.contains("lic")`,
		`user.name.startsWith("Al")`,
		`user.name.matches("^[A-Z][a-z]+$")`,
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
	// user.name.endsWith("ice") -> true
	// "admin" in user.roles -> true
	// user.age >= 18 -> true
	// user.score > 90.0 -> true
	// user.name + " (" + string(user.age) + ")" -> Alice (30)
	// size(user.roles) -> 3
	// user.name.contains("lic") -> true
	// user.name.startsWith("Al") -> true
	// user.name.matches("^[A-Z][a-z]+$") -> true
}

// Example_cel_Strings showcases built-in string manipulation functions
// as documented in https://celbyexample.com/strings/
func Example_cel_Strings() {
	exprs := []string{
		`"hello" + " " + "world"`,
		`size("hello")`,
		`"hello".size()`,
		`"hello world".contains("world")`,
		`"hello world".endsWith("world")`,
		`"hello world".startsWith("hello")`,
		`"hello".matches("^[a-z]+$")`,
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
	// "hello" + " " + "world" -> hello world
	// size("hello") -> 5
	// "hello".size() -> 5
	// "hello world".contains("world") -> true
	// "hello world".endsWith("world") -> true
	// "hello world".startsWith("hello") -> true
	// "hello".matches("^[a-z]+$") -> true
}

// Example_cel_StringsExtension showcases string extension library functions (ext.Strings())
// as documented in https://celbyexample.com/strings/
func Example_cel_StringsExtension() {
	exprs := []string{
		`"hello world".replace("world", "CEL")`,
		`"hello world".substring(0, 5)`,
		`"HELLO".lowerAscii()`,
		`"hello".upperAscii()`,
		`"  hello  ".trim()`,
		`"hello".charAt(0)`,
		`"hello world".indexOf("world")`,
		`"a,b,c".split(",")`,
		`["a", "b", "c"].join("-")`,
	}

	for _, expr := range exprs {
		prg, err := cel.Compile(expr, ext.Strings())
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
	// "hello world".replace("world", "CEL") -> hello CEL
	// "hello world".substring(0, 5) -> hello
	// "HELLO".lowerAscii() -> hello
	// "hello".upperAscii() -> HELLO
	// "  hello  ".trim() -> hello
	// "hello".charAt(0) -> h
	// "hello world".indexOf("world") -> 6
	// "a,b,c".split(",") -> [a, b, c]
	// ["a", "b", "c"].join("-") -> a-b-c
}

// Example_cel_Primitives showcases int, uint, double, bool, bytes, and string primitive literals
// as documented in https://celbyexample.com/primitives/
func Example_cel_Primitives() {
	exprs := []string{
		`42`,
		`-10`,
		`42u`,
		`3.14159`,
		`1e-3`,
		`true`,
		`false`,
		`"hello CEL"`,
		`b"hello"`,
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
	// 42 -> 42
	// -10 -> -10
	// 42u -> 42
	// 3.14159 -> 3.14159
	// 1e-3 -> 0.001
	// true -> true
	// false -> false
	// "hello CEL" -> hello CEL
	// b"hello" -> [104 101 108 108 111]
}
