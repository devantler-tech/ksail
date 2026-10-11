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

// Example_cel_Collections showcases membership, indexing, search (exists), all, and filter
// on complex collections as documented in https://celbyexample.com/collections/
func Example_cel_Collections() {
	orderData := map[string]any{
		"id":       "ord-123",
		"customer": "alice@example.com",
		"tags":     []string{"express", "gift-wrapped"},
		"items": []map[string]any{
			{"name": "Laptop", "category": "electronics", "price": 999, "quantity": 1},
			{"name": "Cable", "category": "accessories", "price": 29, "quantity": 3},
		},
		"status": "pending",
	}

	exprs := []string{
		`"express" in order.tags`,
		`size(order.tags)`,
		`size(order.items)`,
		`order.tags[0]`,
		`order.items[0].name`,
		`order.items[0].price`,
		`order.tags.exists(t, t.startsWith("ex"))`,
		`order.items.exists(i, i.price > 500)`,
		`order.items.exists(i, i.category == "electronics" && i.price > 100)`,
		`order.tags.all(t, size(t) > 0)`,
		`order.items.all(i, i.quantity > 0)`,
		`order.items.all(i, i.price < 10000)`,
		`order.tags.filter(t, t != "express")`,
		`order.items.filter(i, i.price > 100)[0].name`,
		`order.items.filter(i, i.category == "electronics")[0].name`,
	}

	for _, expr := range exprs {
		prg, err := cel.Compile(expr, cel.Variable("order", cel.DynType))
		if err != nil {
			log.Fatalf("cel.Compile() error for %q: %v", expr, err)
		}
		out, _, err := prg.Eval(map[string]any{"order": orderData})
		if err != nil {
			log.Fatalf("prg.Eval() error for %q: %v", expr, err)
		}
		fmt.Printf("%s -> %v\n", expr, out)
	}

	// Output:
	// "express" in order.tags -> true
	// size(order.tags) -> 2
	// size(order.items) -> 2
	// order.tags[0] -> express
	// order.items[0].name -> Laptop
	// order.items[0].price -> 999
	// order.tags.exists(t, t.startsWith("ex")) -> true
	// order.items.exists(i, i.price > 500) -> true
	// order.items.exists(i, i.category == "electronics" && i.price > 100) -> true
	// order.tags.all(t, size(t) > 0) -> true
	// order.items.all(i, i.quantity > 0) -> true
	// order.items.all(i, i.price < 10000) -> true
	// order.tags.filter(t, t != "express") -> [gift-wrapped]
	// order.items.filter(i, i.price > 100)[0].name -> Laptop
	// order.items.filter(i, i.category == "electronics")[0].name -> Laptop
}

// Example_cel_Lists showcases list literals, indexing, membership, concatenation, and equality
// as documented in https://celbyexample.com/lists/
func Example_cel_Lists() {
	exprs := []string{
		`[1, 2, 3]`,
		`[]`,
		`[1, "two", true]`,
		`[[1, 2], [3, 4]]`,
		`size([1, 2, 3])`,
		`[1, 2, 3].size()`,
		`["a", "b", "c"][0]`,
		`["a", "b", "c"][2]`,
		`[[1, 2], [3, 4]][0][1]`,
		`2 in [1, 2, 3]`,
		`5 in [1, 2, 3]`,
		`[1, 2] + [3, 4]`,
		`[1, 2, 3] == [1, 2, 3]`,
		`[1, 2, 3] == [3, 2, 1]`,
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
	// [1, 2, 3] -> [1, 2, 3]
	// [] -> []
	// [1, "two", true] -> [1, two, true]
	// [[1, 2], [3, 4]] -> [[1, 2], [3, 4]]
	// size([1, 2, 3]) -> 3
	// [1, 2, 3].size() -> 3
	// ["a", "b", "c"][0] -> a
	// ["a", "b", "c"][2] -> c
	// [[1, 2], [3, 4]][0][1] -> 2
	// 2 in [1, 2, 3] -> true
	// 5 in [1, 2, 3] -> false
	// [1, 2] + [3, 4] -> [1, 2, 3, 4]
	// [1, 2, 3] == [1, 2, 3] -> true
	// [1, 2, 3] == [3, 2, 1] -> false
}

// Example_cel_Maps showcases map literals, size, field/key access, membership, and comparison
// as documented in https://celbyexample.com/maps/
func Example_cel_Maps() {
	exprs := []string{
		`{"a": 1, "b": 2}.a`,
		`size({"a": 1, "b": 2})`,
		`{1: "one", 2: "two"}[1]`,
		`{"nested": {"a": 1}}.nested.a`,
		`size({"a": 1, "b": 2, "c": 3})`,
		`{"a": 1, "b": 2}.size()`,
		`{"a": 1, "b": 2}["a"]`,
		`{"a": 1, "b": 2}.a`,
		`"a" in {"a": 1, "b": 2}`,
		`"c" in {"a": 1, "b": 2}`,
		`{"a": 1, "b": 2} == {"a": 1, "b": 2}`,
		`{"a": 1} == {"a": 2}`,
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
	// {"a": 1, "b": 2}.a -> 1
	// size({"a": 1, "b": 2}) -> 2
	// {1: "one", 2: "two"}[1] -> one
	// {"nested": {"a": 1}}.nested.a -> 1
	// size({"a": 1, "b": 2, "c": 3}) -> 3
	// {"a": 1, "b": 2}.size() -> 2
	// {"a": 1, "b": 2}["a"] -> 1
	// {"a": 1, "b": 2}.a -> 1
	// "a" in {"a": 1, "b": 2} -> true
	// "c" in {"a": 1, "b": 2} -> false
	// {"a": 1, "b": 2} == {"a": 1, "b": 2} -> true
	// {"a": 1} == {"a": 2} -> false
}
