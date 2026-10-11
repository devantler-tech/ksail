// Copyright 2018 Google LLC
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

// package common defines types common to parsing and other diagnostics.
package common

import (
	"strings"
	"testing"
)

// TestErrors reporting and recording.
func TestErrors(t *testing.T) {
	source := NewStringSource("a.b\n&&arg(missing, paren", "errors-test")
	errors := NewErrors(source)
	errors.ReportError(NewLocation(1, 1), "No such field")
	if len(errors.GetErrors()) != 1 {
		t.Errorf("%s first error not recorded", t.Name())
	}
	errors.ReportError(NewLocation(2, 20),
		"Syntax error, missing paren")
	if len(errors.GetErrors()) != 2 {
		t.Errorf("%s second error not recorded", t.Name())
	}
	got := errors.ToDisplayString()
	want :=
		"ERROR: errors-test:1:2: No such field\n" +
			" | a.b\n" +
			" | .^\n" +
			"ERROR: errors-test:2:21: Syntax error, missing paren\n" +
			" | &&arg(missing, paren\n" +
			" | ....................^"
	if got != want {
		t.Errorf("%s got %s, wanted %s", t.Name(), got, want)
	}
}

func TestErrorsReportingLimit(t *testing.T) {
	errors := NewErrors(NewTextSource("hello world"))
	for i := 0; i < 2*errors.maxErrorsToReport; i++ {
		errors.ReportError(NoLocation, "error %d", i)
	}
	if !strings.HasSuffix(errors.ToDisplayString(), "100 more errors were truncated") {
		t.Errorf("Error truncation did not succeed, got %s, wanted 100 errors truncated", errors.ToDisplayString())
	}
}

func TestErrorsAppendReportingLimit(t *testing.T) {
	errors := NewErrors(NewTextSource("hello world"))
	for i := 0; i < 75; i++ {
		errors.ReportError(NoLocation, "error %d", i)
	}
	errors2 := NewErrors(NewTextSource("hello world"))
	for i := 0; i < 75; i++ {
		errors2.ReportError(NoLocation, "error %d", i+75)
	}
	errors = errors.Append(errors2.GetErrors())
	if !strings.HasSuffix(errors.ToDisplayString(), "50 more errors were truncated") {
		t.Errorf("Error truncation did not succeed, got %s, wanted 50 errors truncated", errors.ToDisplayString())
	}
}

func TestErrors_WideAndNarrowCharacters(t *testing.T) {
	source := NewStringSource("你好吗\n我a很好\n", "errors-test")
	errors := NewErrors(source)
	errors.ReportError(NewLocation(2, 3), "Unexpected character '好'")

	got := errors.ToDisplayString()
	want := "ERROR: errors-test:2:4: Unexpected character '好'\n" +
		" | 我a很好\n" +
		" | ．.．＾"
	if got != want {
		t.Errorf("%s got %s, wanted %s", t.Name(), got, want)
	}
}

func TestErrors_WideAndNarrowCharactersWithEmojis(t *testing.T) {
	source := NewStringSource("      '😁' in ['😁', '😑', '😦'] && in.😁", "errors-test")
	errors := NewErrors(source)
	errors.ReportError(NewLocation(1, 32), "Syntax error: extraneous input 'in' expecting {'[', '{', '(', '.', '-', '!', 'true', 'false', 'null', NUM_FLOAT, NUM_INT, NUM_UINT, STRING, BYTES, IDENTIFIER}")
	errors.ReportError(NewLocation(1, 35), "Syntax error: token recognition error at: '😁'")
	errors.ReportError(NewLocation(1, 36), "Syntax error: missing IDENTIFIER at '<EOF>'")
	got := errors.ToDisplayString()
	want := "ERROR: errors-test:1:33: Syntax error: extraneous input 'in' expecting {'[', '{', '(', '.', '-', '!', 'true', 'false', 'null', NUM_FLOAT, NUM_INT, NUM_UINT, STRING, BYTES, IDENTIFIER}\n" +
		" |       '😁' in ['😁', '😑', '😦'] && in.😁\n" +
		" | .......．.......．....．....．......^\n" +
		"ERROR: errors-test:1:36: Syntax error: token recognition error at: '😁'\n" +
		" |       '😁' in ['😁', '😑', '😦'] && in.😁\n" +
		" | .......．.......．....．....．.........＾\n" +
		"ERROR: errors-test:1:37: Syntax error: missing IDENTIFIER at '<EOF>'\n" +
		" |       '😁' in ['😁', '😑', '😦'] && in.😁\n" +
		" | .......．.......．....．....．.........．^"
	if got != want {
		t.Errorf("%s got %s, wanted %s", t.Name(), got, want)
	}
}
