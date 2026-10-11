// Copyright 2023 Google LLC
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

package types

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/google/cel-go/common/types/ref"
)

func TestIsUnknown(t *testing.T) {
	if IsUnknown(&Unknown{}) != true {
		t.Error("IsUnknown(Unknown{}) returned false, wanted true")
	}
	if IsUnknown(Bool(true)) != false {
		t.Error("IsUnknown(true) returned true, wanted false")
	}
}

func TestNewAttribute(t *testing.T) {
	if NewAttributeTrail("") != unspecifiedAttribute {
		t.Error("An empty attribute must be the unspecified attribute")
	}
	if NewAttributeTrail("v").Equal(unspecifiedAttribute) {
		t.Error("A non-empty attribute must not be equal to the unspecified attribute")
	}
}

func TestAttributeEquals(t *testing.T) {
	tests := []struct {
		a     *AttributeTrail
		b     *AttributeTrail
		equal bool
	}{
		{
			a:     unspecifiedAttribute,
			b:     NewAttributeTrail(""),
			equal: true,
		},
		{
			a:     NewAttributeTrail("a"),
			b:     NewAttributeTrail(""),
			equal: false,
		},
		{
			a:     NewAttributeTrail("a"),
			b:     NewAttributeTrail("a"),
			equal: true,
		},
		{
			a:     QualifyAttribute[string](NewAttributeTrail("a"), "b"),
			b:     NewAttributeTrail("a"),
			equal: false,
		},
		{
			a:     QualifyAttribute[string](NewAttributeTrail("a"), "b"),
			b:     QualifyAttribute[int64](NewAttributeTrail("a"), 1),
			equal: false,
		},
		{
			a:     QualifyAttribute[int64](NewAttributeTrail("a"), 1),
			b:     QualifyAttribute[string](NewAttributeTrail("a"), "1"),
			equal: false,
		},
		{
			a:     QualifyAttribute[uint64](NewAttributeTrail("a"), 1),
			b:     QualifyAttribute[string](NewAttributeTrail("a"), "1"),
			equal: false,
		},
		{
			a:     QualifyAttribute[string](NewAttributeTrail("a"), "b"),
			b:     QualifyAttribute[string](NewAttributeTrail("a"), "b"),
			equal: true,
		},
		{
			a:     QualifyAttribute[int64](NewAttributeTrail("a"), 20),
			b:     QualifyAttribute[uint64](NewAttributeTrail("a"), 20),
			equal: true,
		},
		{
			a:     QualifyAttribute[uint64](NewAttributeTrail("a"), 20),
			b:     QualifyAttribute[int64](NewAttributeTrail("a"), 20),
			equal: true,
		},
		{
			a:     QualifyAttribute[uint64](NewAttributeTrail("a"), 21),
			b:     QualifyAttribute[int64](NewAttributeTrail("a"), 20),
			equal: false,
		},
		{
			a:     QualifyAttribute[int64](NewAttributeTrail("a"), 20),
			b:     QualifyAttribute[uint64](NewAttributeTrail("a"), 21),
			equal: false,
		},
		{
			a:     QualifyAttribute[int64](NewAttributeTrail("a"), -1),
			b:     QualifyAttribute[uint64](NewAttributeTrail("a"), 0),
			equal: false,
		},
		{
			a:     QualifyAttribute[int64](NewAttributeTrail("a"), 1),
			b:     QualifyAttribute[uint64](NewAttributeTrail("a"), math.MaxInt64+1),
			equal: false,
		},
	}
	for i, tst := range tests {
		tc := tst
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			out := tc.a.Equal(tc.b)
			if out != tc.equal {
				t.Errorf("%v.Equal(%v) got %v, wanted %v", tc.a, tc.b, out, tc.equal)
			}
		})
	}
}

func TestAttributeString(t *testing.T) {
	tests := []struct {
		attr *AttributeTrail
		out  string
	}{
		{
			attr: unspecifiedAttribute,
			out:  "<unspecified>",
		},
		{
			attr: NewAttributeTrail("a"),
			out:  "a",
		},
		{
			attr: QualifyAttribute[bool](NewAttributeTrail("a"), false),
			out:  "a[false]",
		},
		{
			attr: QualifyAttribute[string](NewAttributeTrail("a"), "b"),
			out:  "a.b",
		},
		{
			attr: QualifyAttribute(QualifyAttribute[string](NewAttributeTrail("a"), "b"), "$this"),
			out:  `a.b["$this"]`,
		},
		{
			attr: QualifyAttribute[int64](NewAttributeTrail("a"), 12),
			out:  "a[12]",
		},
		{
			attr: QualifyAttribute[uint64](NewAttributeTrail("a"), 24),
			out:  "a[24u]",
		},
	}
	for i, tst := range tests {
		tc := tst
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			out := tc.attr.String()
			if out != tc.out {
				t.Errorf("%v.String() got %v, wanted %v", tc.attr, out, tc.out)
			}
		})
	}
}

func TestUnknownContains(t *testing.T) {
	tests := []struct {
		unk   *Unknown
		other *Unknown
		out   bool
	}{
		{
			unk:   NewUnknown(1, nil),
			other: NewUnknown(1, unspecifiedAttribute),
			out:   true,
		},
		{
			unk:   NewUnknown(3, QualifyAttribute[bool](NewAttributeTrail("a"), true)),
			other: NewUnknown(4, QualifyAttribute[string](NewAttributeTrail("a"), "b")),
			out:   false,
		},
		{
			unk:   NewUnknown(3, QualifyAttribute[string](NewAttributeTrail("a"), "b")),
			other: NewUnknown(4, QualifyAttribute[string](NewAttributeTrail("a"), "b")),
			out:   false,
		},
		{
			unk:   NewUnknown(3, QualifyAttribute[string](NewAttributeTrail("a"), "c")),
			other: NewUnknown(3, QualifyAttribute[string](NewAttributeTrail("a"), "b")),
			out:   false,
		},
		{
			unk: MergeUnknowns(
				NewUnknown(3, QualifyAttribute[bool](NewAttributeTrail("a"), true)),
				NewUnknown(4, QualifyAttribute[string](NewAttributeTrail("a"), "b")),
			),
			other: NewUnknown(3, QualifyAttribute[bool](NewAttributeTrail("a"), true)),
			out:   true,
		},
		{
			unk: NewUnknown(3, QualifyAttribute[bool](NewAttributeTrail("a"), true)),
			other: MergeUnknowns(
				NewUnknown(3, QualifyAttribute[bool](NewAttributeTrail("a"), true)),
				NewUnknown(4, QualifyAttribute[string](NewAttributeTrail("a"), "b")),
			),
			out: false,
		},
	}
	for i, tst := range tests {
		tc := tst
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			out := tc.unk.Contains(tc.other)
			if out != tc.out {
				t.Errorf("%v.Contains(%v) got %v, wanted %v", tc.unk, tc.other, out, tc.out)
			}
		})
	}
}

func TestUnknownIDs(t *testing.T) {
	tests := []struct {
		unk   *Unknown
		ids   []int64
		attrs []string
	}{
		{
			unk:   NewUnknown(1, nil),
			ids:   []int64{1},
			attrs: []string{"<unspecified>"},
		},
		{
			unk:   NewUnknown(2, QualifyAttribute[bool](NewAttributeTrail("a"), true)),
			ids:   []int64{2},
			attrs: []string{"a[true]"},
		},
		{
			unk:   NewUnknown(3, QualifyAttribute[string](NewAttributeTrail("a"), "b")),
			ids:   []int64{3},
			attrs: []string{"a.b"},
		},
		{
			unk:   NewUnknown(4, QualifyAttribute[string](NewAttributeTrail("a"), "c")),
			ids:   []int64{4},
			attrs: []string{"a.c"},
		},
		{
			unk: MergeUnknowns(
				NewUnknown(4, QualifyAttribute[string](NewAttributeTrail("a"), "b")),
				NewUnknown(3, QualifyAttribute[bool](NewAttributeTrail("a"), true)),
			),
			ids:   []int64{3, 4},
			attrs: []string{"a[true]", "a.b"},
		},
	}
	for i, tst := range tests {
		tc := tst
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			ids := tc.unk.IDs()
			if !reflect.DeepEqual(ids, tc.ids) {
				t.Errorf("%v.IDs() got %v, wanted %v", tc.unk, ids, tc.ids)
			}
			attrs := make([]string, len(ids))
			idx := 0
			for _, id := range ids {
				trails, found := tc.unk.GetAttributeTrails(id)
				if !found {
					t.Fatalf("GetAttributeTrails(%d) not found", id)
				}
				if len(trails) != 1 {
					t.Fatalf("GetAttributeTrails(%d) got %d trails, wanted 1", id, len(trails))
				}
				attrs[idx] = trails[0].String()
				idx++
			}
			if !reflect.DeepEqual(attrs, tc.attrs) {
				t.Errorf("%v.GetAttributeTrails() got %v, wanted %v", tc.unk, attrs, tc.attrs)
			}
		})
	}
}

func TestUnknownString(t *testing.T) {
	tests := []struct {
		unk *Unknown
		out any
	}{
		{
			unk: NewUnknown(1, nil),
			out: "<unspecified> (1)",
		},
		{
			unk: NewUnknown(1, unspecifiedAttribute),
			out: "<unspecified> (1)",
		},
		{
			unk: NewUnknown(2, NewAttributeTrail("a")),
			out: "a (2)",
		},
		{
			unk: NewUnknown(3, QualifyAttribute[bool](NewAttributeTrail("a"), false)),
			out: "a[false] (3)",
		},
		{
			unk: MergeUnknowns(
				NewUnknown(3, QualifyAttribute[bool](NewAttributeTrail("a"), true)),
				NewUnknown(4, QualifyAttribute[string](NewAttributeTrail("a"), "b")),
			),
			out: []string{"a[true] (3)", "a.b (4)"},
		},
		{
			// this case might occur in a logical condition where the attributes are equal.
			unk: MergeUnknowns(
				NewUnknown(3, QualifyAttribute[int64](NewAttributeTrail("a"), 0)),
				NewUnknown(3, QualifyAttribute[int64](NewAttributeTrail("a"), 0)),
			),
			out: "a[0] (3)",
		},
		{
			// this case might occur if attribute tracking through comprehensions is supported
			unk: MergeUnknowns(
				NewUnknown(3, QualifyAttribute[int64](NewAttributeTrail("a"), 0)),
				NewUnknown(3, QualifyAttribute[int64](NewAttributeTrail("a"), 1)),
			),
			out: "[a[0] a[1]] (3)",
		},
	}
	for i, tst := range tests {
		tc := tst
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			out := tc.unk.String()
			switch want := tc.out.(type) {
			case string:
				if out != want {
					t.Errorf("%v.String() got %v, wanted %v", tc.unk, out, want)
				}
			case []string:
				for _, w := range want {
					if !strings.Contains(out, w) {
						t.Errorf("%v.String() got %v, wanted it to contain %v", tc.unk, out, w)
					}
				}
			}

		})
	}
}

func TestMaybeMergeUnknowns(t *testing.T) {
	tests := []struct {
		in    ref.Val
		unk   *Unknown
		want  *Unknown
		isUnk bool
	}{
		// one of the unknowns is empty
		{
			in:    String(""),
			unk:   nil,
			isUnk: false,
		},
		// both unknowns are empty
		{
			in:    String(""),
			unk:   &Unknown{},
			want:  &Unknown{},
			isUnk: true,
		},
		{
			in:    newUnk(t, 2, "x"),
			unk:   newUnk(t, 1, "y"),
			want:  MergeUnknowns(newUnk(t, 2, "x"), newUnk(t, 1, "y")),
			isUnk: true,
		},
		{
			in:    newUnk(t, 2, "x"),
			want:  newUnk(t, 2, "x"),
			isUnk: true,
		},
	}

	for i, tst := range tests {
		tc := tst
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			got, isUnk := MaybeMergeUnknowns(tc.in, tc.unk)
			if isUnk != tc.isUnk {
				t.Errorf("MaybeMergeUnknowns(%v, %v) got %v, wanted %v", tc.in, tc.unk, got, tc.want)
			}
		})
	}
}

func newUnk(t *testing.T, id int64, varName string) *Unknown {
	t.Helper()
	return NewUnknown(id, NewAttributeTrail(varName))
}
