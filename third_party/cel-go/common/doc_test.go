// Copyright 2025 Google LLC
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

package common

import (
	"reflect"
	"testing"
)

func TestParseDescription(t *testing.T) {
	tests := []struct {
		name string
		in   string
		out  string
	}{
		{
			name: "empty",
		},
		{
			name: "single",
			in:   "hello",
			out:  "hello",
		},
		{
			name: "multi",
			in:   "hello\n\n\nworld",
			out:  "hello\nworld",
		},
	}

	for _, tst := range tests {
		tc := tst
		t.Run(tc.name, func(t *testing.T) {
			out := ParseDescription(tc.in)
			if !reflect.DeepEqual(out, tc.out) {
				t.Errorf("ParseDescription() got %v, wanted %v", out, tc.out)
			}
		})
	}
}

func TestParseDescriptions(t *testing.T) {
	tests := []struct {
		name string
		in   string
		out  []string
	}{
		{
			name: "empty",
		},
		{
			name: "single",
			in:   "hello",
			out:  []string{"hello"},
		},
		{
			name: "multi",
			in:   "bar\nbaz\n\nfoo",
			out:  []string{"bar\nbaz", "foo"},
		},
		{
			name: "multi",
			in:   "hello\n\n\nworld",
			out:  []string{"hello", "world"},
		},
	}

	for _, tst := range tests {
		tc := tst
		t.Run(tc.name, func(t *testing.T) {
			out := ParseDescriptions(tc.in)
			if !reflect.DeepEqual(out, tc.out) {
				t.Errorf("ParseDescriptions() got %v, wanted %v", out, tc.out)
			}
		})
	}
}

func TestNewDoc(t *testing.T) {
	tests := []struct {
		newDoc     func() *Doc
		kind       DocKind
		name       string
		celType    string
		sig        string
		desc       string
		childCount int
	}{
		{
			newDoc: func() *Doc {
				return NewMacroDoc("map", "map converts a list or map of values to a list",
					NewExampleDoc("[1, 2].map(i, i * 2) // [2, 4]"))
			},
			kind:       DocMacro,
			name:       "map",
			desc:       "map converts a list or map of values to a list",
			childCount: 1,
		},
		{
			newDoc: func() *Doc {
				return NewVariableDoc(
					"request",
					"google.rpc.context.AttributeContext.Request",
					"parameters related to an HTTP API request")
			},
			kind:       DocVariable,
			name:       "request",
			celType:    "google.rpc.context.AttributeContext.Request",
			desc:       "parameters related to an HTTP API request",
			childCount: 0,
		},
		{
			newDoc: func() *Doc {
				return NewFunctionDoc("getToken",
					"get the JWT token from a request\nas deserialized JSON",
					NewOverloadDoc("request_getToken", "request.getToken() -> map(string, dyn)",
						NewExampleDoc("has(request.getToken().sub) // false")))
			},
			kind:       DocFunction,
			name:       "getToken",
			desc:       "get the JWT token from a request\nas deserialized JSON",
			childCount: 1,
		},
		{
			newDoc: func() *Doc {
				return NewFieldDoc("google.rpc.context.AttributeContext.Request.token", "string",
					"The HTTP URL scheme, such as `http` and `https`.",
					NewExampleDoc("\"https\""))
			},
			kind:       DocField,
			name:       "google.rpc.context.AttributeContext.Request.token",
			celType:    "string",
			desc:       "The HTTP URL scheme, such as `http` and `https`.",
			childCount: 1,
		},
	}

	for _, tst := range tests {
		tc := tst
		t.Run(tc.name, func(t *testing.T) {
			d := tc.newDoc()
			if d.Kind != tc.kind {
				t.Errorf("got doc kind %v, wanted %v", d.Kind, tc.kind)
			}
			if d.Name != tc.name {
				t.Errorf("got doc name %s, wanted %s", d.Name, tc.name)
			}
			if d.Signature != tc.sig {
				t.Errorf("got signature %s, wanted %s", d.Signature, tc.sig)
			}
			if !reflect.DeepEqual(d.Description, tc.desc) {
				t.Errorf("got description %v, wanted %v", d.Description, tc.desc)
			}
			if d.Type != tc.celType {
				t.Errorf("got type %s, wanted %s", d.Type, tc.celType)
			}
			if len(d.Children) != tc.childCount {
				t.Errorf("got children %v, wanted count %d", d.Children, tc.childCount)
			}
		})
	}
}
