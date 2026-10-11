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

// Example_cel_TransformingData showcases building maps, transforming lists with map(),
// filtering and transforming, and combining techniques as documented in https://celbyexample.com/transforming-data/
func Example_cel_TransformingData() {
	orderData := map[string]any{
		"id":           "ord-123",
		"customer":     "alice@example.com",
		"customer_age": 30,
		"tags":         []string{"express", "gift-wrapped"},
		"items": []map[string]any{
			{"name": "Laptop", "category": "electronics", "price": 999, "quantity": 1},
			{"name": "Cable", "category": "accessories", "price": 29, "quantity": 3},
		},
		"status": "pending",
	}

	exprs := []string{
		`{"tags": order.tags, "is_adult": order.customer_age >= 18}.is_adult`,
		`{"customer": order.customer, "item_count": size(order.items)}.item_count`,
		`order.tags.map(t, t + ":enabled")`,
		`order.items.map(i, i.name)`,
		`order.tags.map(t, t != "express", t + ":applied")`,
		`order.items.map(i, i.price > 100, i.name)`,
		`{"id": order.id, "electronics": order.items.filter(i, i.category == "electronics").map(i, i.name)}.electronics`,
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
	// {"tags": order.tags, "is_adult": order.customer_age >= 18}.is_adult -> true
	// {"customer": order.customer, "item_count": size(order.items)}.item_count -> 2
	// order.tags.map(t, t + ":enabled") -> [express:enabled, gift-wrapped:enabled]
	// order.items.map(i, i.name) -> [Laptop, Cable]
	// order.tags.map(t, t != "express", t + ":applied") -> [gift-wrapped:applied]
	// order.items.map(i, i.price > 100, i.name) -> [Laptop]
	// {"id": order.id, "electronics": order.items.filter(i, i.category == "electronics").map(i, i.name)}.electronics -> [Laptop]
}

// Example_cel_Macros showcases built-in CEL macros (has, all, exists, exists_one, filter, map)
// as documented in https://celbyexample.com/has/, /all/, /exists/, /exists-one/, /filter/, /map-macro/
func Example_cel_Macros() {
	vars := map[string]any{
		"msg":     map[string]any{"field": "present", "zero": 0},
		"numbers": []int64{1, 2, 3, 4, 5},
	}

	exprs := []string{
		`has(msg.field)`,
		`has(msg.missing)`,
		`numbers.all(x, x > 0)`,
		`numbers.all(x, x % 2 == 0)`,
		`numbers.exists(x, x == 3)`,
		`numbers.exists(x, x > 10)`,
		`numbers.exists_one(x, x == 3)`,
		`numbers.exists_one(x, x % 2 == 0)`,
		`numbers.filter(x, x % 2 == 0)`,
		`numbers.map(x, x * 10)`,
		`numbers.map(x, x % 2 == 0, x * 10)`,
	}

	opts := []cel.EnvOption{
		cel.Variable("msg", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("numbers", cel.ListType(cel.IntType)),
	}

	for _, expr := range exprs {
		prg, err := cel.Compile(expr, opts...)
		if err != nil {
			log.Fatalf("cel.Compile() error for %q: %v", expr, err)
		}
		out, _, err := prg.Eval(vars)
		if err != nil {
			log.Fatalf("prg.Eval() error for %q: %v", expr, err)
		}
		fmt.Printf("%s -> %v\n", expr, out)
	}

	// Output:
	// has(msg.field) -> true
	// has(msg.missing) -> false
	// numbers.all(x, x > 0) -> true
	// numbers.all(x, x % 2 == 0) -> false
	// numbers.exists(x, x == 3) -> true
	// numbers.exists(x, x > 10) -> false
	// numbers.exists_one(x, x == 3) -> true
	// numbers.exists_one(x, x % 2 == 0) -> false
	// numbers.filter(x, x % 2 == 0) -> [2, 4]
	// numbers.map(x, x * 10) -> [10, 20, 30, 40, 50]
	// numbers.map(x, x % 2 == 0, x * 10) -> [20, 40]
}
