// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package test provides a YAML-serializable test suite for CEL tests.
package test

// Suite is a collection of tests designed to evaluate the correctness of
// a CEL policy or a CEL expression
type Suite struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Sections    []*Section `yaml:"section"`
}

// Section is a collection of related test cases.
type Section struct {
	Name  string  `yaml:"name"`
	Tests []*Case `yaml:"tests"`
}

// Case is a test case to validate a CEL policy or expression. The test case
// encompasses evaluation of the compiled expression using the provided input
// bindings and asserting the result against the expected result.
type Case struct {
	Name          string                 `yaml:"name"`
	Description   string                 `yaml:"description"`
	Input         map[string]*InputValue `yaml:"input,omitempty"`
	*InputContext `yaml:",inline,omitempty"`
	Output        *Output `yaml:"output"`
}

// InputContext represents an optional context expression.
type InputContext struct {
	ContextExpr string `yaml:"context_expr"`
}

// InputValue represents an input value for a binding which can be either a simple literal value or
// an expression.
type InputValue struct {
	Value any    `yaml:"value"`
	Expr  string `yaml:"expr"`
}

// Output represents the expected result of a test case.
type Output struct {
	Value      any      `yaml:"value"`
	Expr       string   `yaml:"expr"`
	ErrorSet   []string `yaml:"error_set"`
	UnknownSet []int64  `yaml:"unknown_set"`
}
