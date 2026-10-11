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
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker"
	"github.com/google/cel-go/common/types/ref"
)

type exampleCostEstimator struct {
	hints map[string]uint64
}

func (e exampleCostEstimator) EstimateSize(element checker.AstNode) *checker.SizeEstimate {
	if l, ok := e.hints[strings.Join(element.Path(), ".")]; ok {
		return &checker.SizeEstimate{Min: 0, Max: l}
	}
	return nil
}

func (exampleCostEstimator) EstimateCallCost(function, overloadID string, target *checker.AstNode, args []checker.AstNode) *checker.CallEstimate {
	return nil
}

type exampleRuntimeCostEstimator struct{}

func (exampleRuntimeCostEstimator) CallCost(function, overloadID string, args []ref.Val, result ref.Val) *uint64 {
	return nil
}

// Example_cel_ExecutionCost showcases static cost estimation with size hints and dynamic cost limits
// as documented in https://celbyexample.com/execution-cost/
func Example_cel_ExecutionCost() {
	env, err := cel.NewEnv(
		cel.Variable("items", cel.ListType(cel.IntType)),
	)
	if err != nil {
		log.Fatalf("cel.NewEnv() error: %v", err)
	}

	ast, iss := env.Compile(`items.map(x, x * x).filter(x, x > 0).size()`)
	if iss.Err() != nil {
		log.Fatalln(iss.Err())
	}

	// 1. Static cost estimation with hints for "items" list length and "items.@items" element size
	estimator := exampleCostEstimator{
		hints: map[string]uint64{
			"items":        5,
			"items.@items": 10,
		},
	}
	costEstimate, err := env.EstimateCost(ast, estimator)
	if err != nil {
		log.Fatalf("env.EstimateCost() error: %v", err)
	}
	fmt.Printf("Estimated cost range: min=%d max=%d\n", costEstimate.Min, costEstimate.Max)

	// 2. Program with cost tracking and dynamic limit
	prg, err := env.Program(ast,
		cel.CostTracking(exampleRuntimeCostEstimator{}),
		cel.CostLimit(1000),
	)
	if err != nil {
		log.Fatalf("env.Program() error: %v", err)
	}

	out, details, err := prg.Eval(map[string]any{
		"items": []int64{1, 2, 3, 4, 5},
	})
	if err != nil {
		log.Fatalf("prg.Eval() error: %v", err)
	}

	fmt.Printf("Result: %v\n", out)
	if details.ActualCost() != nil {
		fmt.Printf("Actual cost: %d\n", *details.ActualCost())
	}

	// Output:
	// Estimated cost range: min=24 max=174
	// Result: 5
	// Actual cost: 174
}
