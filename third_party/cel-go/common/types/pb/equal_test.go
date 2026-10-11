// Copyright 2022 Google LLC
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

package pb

import (
	"math"
	"testing"

	"google.golang.org/protobuf/proto"

	proto3pb "github.com/google/cel-go/test/proto3pb"
	anypb "google.golang.org/protobuf/types/known/anypb"
)

func TestEqual(t *testing.T) {
	tests := []struct {
		name string
		a    proto.Message
		b    proto.Message
		out  bool
	}{
		{
			name: "EqualEmptyInstances",
			a:    &proto3pb.TestAllTypes{},
			b:    &proto3pb.TestAllTypes{},
			out:  true,
		},
		{
			name: "NotEqualEmptyInstances",
			a:    &proto3pb.TestAllTypes{},
			b:    &proto3pb.NestedTestAllTypes{},
			out:  false,
		},
		{
			name: "EqualScalarFields",
			a: &proto3pb.TestAllTypes{
				SingleBool:   true,
				SingleBytes:  []byte("world"),
				SingleDouble: 3.0,
				SingleFloat:  1.5,
				SingleInt32:  1,
				SingleUint64: 1,
				SingleString: "hello",
			},
			b: &proto3pb.TestAllTypes{
				SingleBool:   true,
				SingleBytes:  []byte("world"),
				SingleDouble: 3.0,
				SingleFloat:  1.5,
				SingleInt32:  1,
				SingleUint64: 1,
				SingleString: "hello",
			},
			out: true,
		},
		{
			name: "NotEqualFloatNan",
			a: &proto3pb.TestAllTypes{
				SingleFloat: float32(math.NaN()),
			},
			b: &proto3pb.TestAllTypes{
				SingleFloat: float32(math.NaN()),
			},
			out: false,
		},
		{
			name: "NotEqualDifferentFieldsSet",
			a: &proto3pb.TestAllTypes{
				SingleInt32: 1,
			},
			b:   &proto3pb.TestAllTypes{},
			out: false,
		},
		{
			name: "NotEqualDifferentFieldsSetReverse",
			a:    &proto3pb.TestAllTypes{},
			b: &proto3pb.TestAllTypes{
				SingleInt32: 1,
			},
			out: false,
		},
		{
			name: "EqualListField",
			a: &proto3pb.TestAllTypes{
				RepeatedInt32: []int32{1, 2, 3, 4},
			},
			b: &proto3pb.TestAllTypes{
				RepeatedInt32: []int32{1, 2, 3, 4},
			},
			out: true,
		},
		{
			name: "NotEqualListFieldDifferentLength",
			a: &proto3pb.TestAllTypes{
				RepeatedInt32: []int32{1, 2, 3},
			},
			b: &proto3pb.TestAllTypes{
				RepeatedInt32: []int32{1, 2, 3, 4},
			},
			out: false,
		},
		{
			name: "NotEqualListFieldDifferentContent",
			a: &proto3pb.TestAllTypes{
				RepeatedInt32: []int32{2, 1},
			},
			b: &proto3pb.TestAllTypes{
				RepeatedInt32: []int32{1, 2},
			},
			out: false,
		},
		{
			name: "EqualMapField",
			a: &proto3pb.TestAllTypes{
				MapInt64NestedType: map[int64]*proto3pb.NestedTestAllTypes{
					1: {
						Child: &proto3pb.NestedTestAllTypes{
							Payload: &proto3pb.TestAllTypes{
								StandaloneEnum: proto3pb.TestAllTypes_BAR,
							},
						},
					},
					2: {
						Payload: &proto3pb.TestAllTypes{},
					},
				},
			},
			b: &proto3pb.TestAllTypes{
				MapInt64NestedType: map[int64]*proto3pb.NestedTestAllTypes{
					1: {
						Child: &proto3pb.NestedTestAllTypes{
							Payload: &proto3pb.TestAllTypes{
								StandaloneEnum: proto3pb.TestAllTypes_BAR,
							},
						},
					},
					2: {
						Payload: &proto3pb.TestAllTypes{},
					},
				},
			},
			out: true,
		},
		{
			name: "NotEqualMapFieldDifferentLength",
			a: &proto3pb.TestAllTypes{
				MapInt64NestedType: map[int64]*proto3pb.NestedTestAllTypes{
					1: {
						Child: &proto3pb.NestedTestAllTypes{},
					},
					2: {
						Payload: &proto3pb.TestAllTypes{},
					},
				},
			},
			b: &proto3pb.TestAllTypes{
				MapInt64NestedType: map[int64]*proto3pb.NestedTestAllTypes{
					1: {
						Child: &proto3pb.NestedTestAllTypes{},
					},
				},
			},
			out: false,
		},
		{
			name: "EqualAnyBytes",
			a: &proto3pb.TestAllTypes{
				SingleAny: packAny(t, &proto3pb.TestAllTypes{
					SingleInt32:   1,
					SingleUint32:  2,
					SingleString:  "three",
					RepeatedInt32: []int32{1, 2, 3},
				}),
			},
			b: &proto3pb.TestAllTypes{
				SingleAny: packAny(t, &proto3pb.TestAllTypes{
					SingleInt32:   1,
					SingleUint32:  2,
					SingleString:  "three",
					RepeatedInt32: []int32{1, 2, 3},
				}),
			},
			out: true,
		},
		{
			name: "NotEqualDoublePackedAny",
			a: &proto3pb.TestAllTypes{
				SingleAny: doublePackAny(t, &proto3pb.TestAllTypes{
					SingleInt32:   1,
					SingleUint32:  2,
					SingleString:  "three",
					RepeatedInt32: []int32{1, 2, 3},
				}),
			},
			b: &proto3pb.TestAllTypes{
				SingleAny: doublePackAny(t, &proto3pb.TestAllTypes{
					SingleInt32:   1,
					SingleUint32:  2,
					SingleString:  "three",
					RepeatedInt32: []int32{1, 2, 3, 4},
				}),
			},
			out: false,
		},
		{
			name: "NotEqualAnyTypeURL",
			a: &proto3pb.TestAllTypes{
				SingleAny: packAny(t, &proto3pb.NestedTestAllTypes{}),
			},
			b: &proto3pb.TestAllTypes{
				SingleAny: packAny(t, &proto3pb.TestAllTypes{}),
			},
			out: false,
		},
		{
			name: "NotEqualAnyFields",
			a: &proto3pb.TestAllTypes{
				SingleAny: packAny(t, &proto3pb.TestAllTypes{
					SingleInt32:   1,
					SingleUint32:  2,
					RepeatedInt32: []int32{1, 2, 3},
				}),
			},
			b: &proto3pb.TestAllTypes{
				SingleAny: packAny(t, &proto3pb.TestAllTypes{
					SingleInt32:   1,
					SingleUint32:  2,
					SingleString:  "three",
					RepeatedInt32: []int32{1, 2, 3},
				}),
			},
			out: false,
		},
		{
			name: "NotEqualAnyDeserializeA",
			a: &proto3pb.TestAllTypes{
				SingleAny: badPackAny(t, &proto3pb.TestAllTypes{
					SingleInt32:   1,
					SingleUint32:  2,
					RepeatedInt32: []int32{1, 2, 3},
				}),
			},
			b: &proto3pb.TestAllTypes{
				SingleAny: badPackAny(t, &proto3pb.TestAllTypes{
					SingleInt32:   1,
					SingleUint32:  2,
					SingleString:  "three",
					RepeatedInt32: []int32{1, 2, 3},
				}),
			},
			out: false,
		},
		{
			name: "EqualUnknownFields",
			a: &proto3pb.TestAllTypes{
				SingleAny: misPackAny(t, &proto3pb.NestedTestAllTypes{
					Child: &proto3pb.NestedTestAllTypes{
						Payload: &proto3pb.TestAllTypes{
							SingleInt32: 1,
						},
					},
				}),
			},
			b: &proto3pb.TestAllTypes{
				SingleAny: misPackAny(t, &proto3pb.NestedTestAllTypes{
					Child: &proto3pb.NestedTestAllTypes{
						Payload: &proto3pb.TestAllTypes{
							SingleInt32: 1,
						},
					},
				}),
			},
			out: true,
		},
		{
			name: "NotEqualUnknownFieldsCount",
			a: &proto3pb.TestAllTypes{
				SingleAny: misPackAny(t, &proto3pb.NestedTestAllTypes{
					Child: &proto3pb.NestedTestAllTypes{
						Payload: &proto3pb.TestAllTypes{
							SingleInt32: 1,
							SingleFloat: 2.0,
						},
					},
				}),
			},
			b: &proto3pb.TestAllTypes{
				SingleAny: misPackAny(t, &proto3pb.NestedTestAllTypes{
					Child: &proto3pb.NestedTestAllTypes{
						Payload: &proto3pb.TestAllTypes{
							SingleInt32: 1,
						},
					},
				}),
			},
			out: false,
		},
		{
			name: "NotEqualUnknownFields",
			a: &proto3pb.TestAllTypes{
				SingleAny: misPackAny(t, &proto3pb.NestedTestAllTypes{
					Child: &proto3pb.NestedTestAllTypes{
						Payload: &proto3pb.TestAllTypes{
							SingleInt64: 2,
						},
					},
				}),
			},
			b: &proto3pb.TestAllTypes{
				SingleAny: misPackAny(t, &proto3pb.NestedTestAllTypes{
					Child: &proto3pb.NestedTestAllTypes{
						Payload: &proto3pb.TestAllTypes{
							SingleInt32: 1,
						},
					},
				}),
			},
			out: false,
		},
		{
			name: "NotEqualOneNil",
			a:    nil,
			b:    &proto3pb.TestAllTypes{},
			out:  false,
		},
		{
			name: "EqualBothNil",
			a:    nil,
			b:    nil,
			out:  true,
		},
	}

	for _, tst := range tests {
		tc := tst
		t.Run(tc.name, func(t *testing.T) {
			got := Equal(tc.a, tc.b)
			if got != tc.out {
				t.Errorf("Equal(%v, %v) got %v, wanted %v", tc.a, tc.b, got, tc.out)
			}
		})
	}
}

func packAny(t *testing.T, m proto.Message) *anypb.Any {
	t.Helper()
	any, err := anypb.New(m)
	if err != nil {
		t.Fatalf("anypb.New(%v) failed with error: %v", m, err)
	}
	return any
}

func doublePackAny(t *testing.T, m proto.Message) *anypb.Any {
	t.Helper()
	any, err := anypb.New(m)
	if err != nil {
		t.Fatalf("anypb.New(%v) failed with error: %v", m, err)
	}
	any, err = anypb.New(any)
	if err != nil {
		t.Fatalf("anypb.New(%v) failed with error: %v", any, err)
	}
	return any
}

func badPackAny(t *testing.T, m proto.Message) *anypb.Any {
	t.Helper()
	any, err := anypb.New(m)
	if err != nil {
		t.Fatalf("anypb.New(%v) failed with error: %v", m, err)
	}
	any.TypeUrl = "type.googleapis.com/BadType"
	return any
}

func misPackAny(t *testing.T, m proto.Message) *anypb.Any {
	t.Helper()
	any, err := anypb.New(m)
	if err != nil {
		t.Fatalf("anypb.New(%v) failed with error: %v", m, err)
	}
	any.TypeUrl = "type.googleapis.com/google.expr.proto3.test.TestAllTypes"
	return any
}
