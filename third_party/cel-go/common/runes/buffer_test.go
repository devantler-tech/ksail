// Copyright 2021 Google LLC
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

package runes

import (
	"testing"
	"unicode/utf8"
)

func TestNewBuffer_ASCII(t *testing.T) {
	data := "hello world!"
	rb := NewBuffer(data)
	if got, want := rb.Len(), utf8.RuneCountInString(data); got != want {
		t.Errorf("length mismatch: got %d, want %d", got, want)
	}
	if got, want := rb.Slice(0, rb.Len()), data; got != want {
		t.Errorf("slice mismatch: got %q, want %q", got, want)
	}
	if got, want := rb.Get(8), rune('r'); got != want {
		t.Errorf("rune mismatch: got %U, want %U", got, want)
	}
	if _, ok := rb.(*asciiBuffer); !ok {
		t.Errorf("type mismatch: got %T, want %T", rb, &asciiBuffer{})
	}
}

func TestNewBuffer_Basic(t *testing.T) {
	data := "hello w\u04E7rld!"
	rb := NewBuffer(data)
	if got, want := rb.Len(), utf8.RuneCountInString(data); got != want {
		t.Errorf("length mismatch: got %d, want %d", got, want)
	}
	if got, want := rb.Slice(0, rb.Len()), data; got != want {
		t.Errorf("slice mismatch: got %q, want %q", got, want)
	}
	if got, want := rb.Get(8), rune('r'); got != want {
		t.Errorf("rune mismatch: got %U, want %U", got, want)
	}
	if _, ok := rb.(*basicBuffer); !ok {
		t.Errorf("type mismatch: got %T, want %T", rb, &basicBuffer{})
	}
}

func TestNewBuffer_Supplemental(t *testing.T) {
	data := "hello w\U0001F642rld!"
	rb := NewBuffer(data)
	if got, want := rb.Len(), utf8.RuneCountInString(data); got != want {
		t.Errorf("length mismatch: got %d, want %d", got, want)
	}
	if got, want := rb.Slice(0, rb.Len()), data; got != want {
		t.Errorf("slice mismatch: got %q, want %q", got, want)
	}
	if got, want := rb.Get(8), rune('r'); got != want {
		t.Errorf("rune mismatch: got %U, want %U", got, want)
	}
	if _, ok := rb.(*supplementalBuffer); !ok {
		t.Errorf("type mismatch: got %T, want %T", rb, &supplementalBuffer{})
	}
}

func TestNewBuffer_All(t *testing.T) {
	data := "hell\u04E7 w\U0001F642rld!"
	rb := NewBuffer(data)
	if got, want := rb.Len(), utf8.RuneCountInString(data); got != want {
		t.Errorf("length mismatch: got %d, want %d", got, want)
	}
	if got, want := rb.Slice(0, rb.Len()), data; got != want {
		t.Errorf("slice mismatch: got %q, want %q", got, want)
	}
	if got, want := rb.Get(8), rune('r'); got != want {
		t.Errorf("rune mismatch: got %U, want %U", got, want)
	}
	if _, ok := rb.(*supplementalBuffer); !ok {
		t.Errorf("type mismatch: got %T, want %T", rb, &supplementalBuffer{})
	}
}

func TestNewBuffer_Empty(t *testing.T) {
	data := ""
	rb := NewBuffer(data)
	if got, want := rb.Len(), utf8.RuneCountInString(data); got != want {
		t.Errorf("length mismatch: got %d, want %d", got, want)
	}
	if got, want := rb.Slice(0, rb.Len()), data; got != want {
		t.Errorf("slice mismatch: got %q, want %q", got, want)
	}
	if _, ok := rb.(*emptyBuffer); !ok {
		t.Errorf("type mismatch: got %T, want %T", rb, &emptyBuffer{})
	}
}

func TestNewBufferAndLineOffsetsWithLimit_Exceeded(t *testing.T) {
	_, _, err := NewBufferAndLineOffsetsWithLimit("greetings", 5)
	if err == nil {
		t.Fatalf("expected size limit error, got nil")
	}
	if got, want := err.Error(), "expression code point size exceeds limit: size: 9, limit 5"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNewBufferAndLineOffsetsWithLimit_MultibyteWithinLimit(t *testing.T) {
	data := "🙂🙂"
	rb, offs, err := NewBufferAndLineOffsetsWithLimit(data, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := rb.Len(), 2; got != want {
		t.Fatalf("length mismatch: got %d, want %d", got, want)
	}
	if got, want := rb.Slice(0, rb.Len()), data; got != want {
		t.Fatalf("slice mismatch: got %q, want %q", got, want)
	}
	if got, want := len(offs), 1; got != want {
		t.Fatalf("line offsets mismatch: got %d, want %d", got, want)
	}
}
