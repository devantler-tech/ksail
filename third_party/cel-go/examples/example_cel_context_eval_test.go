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
	"context"
	"fmt"
	"log"

	"github.com/google/cel-go/cel"
)

// Example_cel_ContextEval showcases evaluation cancellation and timeout using ContextEval
func Example_cel_ContextEval() {
	env, err := cel.NewEnv(
		cel.Variable("items", cel.ListType(cel.IntType)),
	)
	if err != nil {
		log.Fatalf("cel.NewEnv() failed: %v", err)
	}

	ast, iss := env.Compile(`items.map(x, x * 2).filter(x, x >= 50).size()`)
	if iss.Err() != nil {
		log.Fatalf("env.Compile() failed: %v", iss.Err())
	}

	prg, err := env.Program(ast, cel.InterruptCheckFrequency(1))
	if err != nil {
		log.Fatalf("env.Program() failed: %v", err)
	}

	items := make([]int64, 100)
	for i := range items {
		items[i] = int64(i)
	}
	vars := map[string]any{"items": items}

	// 1. Successful evaluation with an active context
	out, _, err := prg.ContextEval(context.Background(), vars)
	if err != nil {
		log.Fatalf("prg.ContextEval() failed: %v", err)
	}
	fmt.Println(out)

	// 2. Interrupted evaluation with a canceled context
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err = prg.ContextEval(canceledCtx, vars)
	if err != nil {
		fmt.Printf("prg.ContextEval() error: %v\n", err)
	}

	// Output:
	// 75
	// prg.ContextEval() error: operation interrupted: context canceled
}
