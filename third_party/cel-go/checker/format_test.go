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

package checker

import (
	"testing"

	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
)

func TestFormatType(t *testing.T) {
	tests := []*types.Type{
		types.AnyType,
		types.BoolType,
		types.BytesType,
		types.DoubleType,
		types.DurationType,
		types.DynType,
		types.ErrorType,
		types.IntType,
		types.NewListType(types.StringType),
		types.NewMapType(types.IntType, types.DynType),
		types.NewObjectType("dev.cel.Expr"),
		types.NewOptionalType(types.BoolType),
		types.NewNullableType(types.IntType),
		types.NewTypeParamType("T"),
		types.NewTypeTypeWithParam(types.NewListType(types.IntType)),
		types.NullType,
		types.StringType,
		types.TimestampType,
		types.TypeType,
		types.UintType,
	}
	for _, tst := range tests {
		tc := tst
		t.Run(tc.DeclaredTypeName(), func(t *testing.T) {
			exprType, err := types.TypeToExprType(tc)
			if err != nil {
				t.Fatalf("types.TypeToExprType(%v) failed: %v", tc, err)
			}
			if FormatCELType(tc) != FormatCheckedType(exprType) {
				t.Errorf("FormatCELType(%v) not equal to FormatCheckedType(%v), got %s, wanted %s",
					tc, exprType, FormatCELType(tc), FormatCheckedType(exprType))
			}
		})
	}
}

func TestFormatFunctionType(t *testing.T) {
	// native type representation of function(string, int) -> bool
	ct := FormatCELType(newFunctionType(types.BoolType, types.StringType, types.IntType))
	// protobuf-based function type
	et := FormatCheckedType(decls.NewFunctionType(decls.Bool, decls.String, decls.Int))
	if ct != et {
		t.Errorf("FormatCELType() not equal to FormatCheckedType(), got %s, wanted %s", ct, et)
	}
}
