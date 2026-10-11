package cel

import (
	"testing"

	tatpb "cel.dev/expr/conformance/proto3"
)

func TestFieldPathsForTestAllTypes(t *testing.T) {
	env, err := NewEnv(
		Types(&tatpb.TestAllTypes{}),
		Variable("t", ObjectType("cel.expr.conformance.proto3.NestedTestAllTypes")),
	)
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}

	p := env.CELTypeProvider()

	paths := fieldPathsForType(p, "t", ObjectType("cel.expr.conformance.proto3.NestedTestAllTypes"))

	tcs := []struct {
		path     string
		typeName string
		isLeaf   bool
	}{
		{
			path:     "t.payload.single_int64",
			typeName: "int",
			isLeaf:   true,
		},
		{
			path:     "t.payload.single_bytes",
			typeName: "bytes",
			isLeaf:   true,
		},
		{
			path:     "t.payload.standalone_enum",
			typeName: "int",
			isLeaf:   true,
		},
		{
			path:     "t.payload.single_bool",
			typeName: "bool",
			isLeaf:   true,
		},
		{
			path:     "t.payload.single_float",
			typeName: "double",
			isLeaf:   true,
		},
		{
			path:     "t.payload.repeated_double",
			typeName: "list(double)",
			isLeaf:   true,
		},
		{
			path:     "t.payload.single_timestamp",
			typeName: "google.protobuf.Timestamp",
			isLeaf:   true,
		},
		{
			path:     "t.payload.single_duration",
			typeName: "google.protobuf.Duration",
			isLeaf:   true,
		},
		{
			path:     "t.payload.single_any",
			typeName: "google.protobuf.Any",
			isLeaf:   true,
		},
		{
			path:     "t.payload.single_struct",
			typeName: "map(string, dyn)",
			isLeaf:   true,
		},
		{
			path:     "t.payload.list_value",
			typeName: "list(dyn)",
			isLeaf:   true,
		},
		{
			path:     "t.payload.single_value",
			typeName: "dyn",
			isLeaf:   true,
		},
		{
			path:     "t.payload.single_int32_wrapper",
			typeName: "wrapper(int)",
			isLeaf:   true,
		},
		{
			path:     "t.child",
			typeName: "cel.expr.conformance.proto3.NestedTestAllTypes",
			isLeaf:   false,
		},
		{
			path:     "t.payload.standalone_message",
			typeName: "cel.expr.conformance.proto3.TestAllTypes.NestedMessage",
			isLeaf:   false,
		},
		{
			path:     "t.payload.standalone_message.bb",
			typeName: "int",
			isLeaf:   true,
		},
		{
			path:     "t.payload.map_string_message",
			typeName: "map(string, cel.expr.conformance.proto3.TestAllTypes.NestedMessage)",
			isLeaf:   false,
		},
		{
			path:     "t.payload.repeated_nested_message[0].bb",
			typeName: "int",
			isLeaf:   true,
		},
		{
			path:     "t.payload.map_string_message[\"\"].bb",
			typeName: "int",
			isLeaf:   true,
		},
		{
			path:     "t.payload.map_int64_message[0].bb",
			typeName: "int",
			isLeaf:   true,
		},
		{
			path:     "t.payload.map_uint64_message[0u].bb",
			typeName: "int",
			isLeaf:   true,
		},
		{
			path:     "t.payload.map_bool_message[false].bb",
			typeName: "int",
			isLeaf:   true,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.path, func(t *testing.T) {
			found := false
			for _, path := range paths {
				if path.path == tc.path {
					found = true
					if path.celType.String() != tc.typeName {
						t.Errorf("path %s has type %s, want %s", tc.path, path.celType.String(), tc.typeName)
					}
					if path.isLeaf != tc.isLeaf {
						t.Errorf("path %s has isLeaf %t, want %t", tc.path, path.isLeaf, tc.isLeaf)
					}
					break
				}
			}
			if !found {
				t.Errorf("path %s not found in field paths", tc.path)
			}
		})
	}

}
