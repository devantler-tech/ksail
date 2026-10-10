# Copyright 2025 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
Starlark rule for triggering unit tests on CEL policies and expressions for go
runtime target.
"""

load("@bazel_skylib//lib:paths.bzl", "paths")
load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

def cel_go_test(
        name,
        test_suite = "",
        config = "",
        test_src = "",
        cel_expr = "",
        is_raw_expr = False,
        base_config = "",
        test_data_path = "",
        file_descriptor_set = "",
        filegroup = "",
        enable_coverage = False,
        deps = [],
        data = [],
        **kwargs):
    """trigger tests for a CEL checked expression

    This macro wraps an invocation of the go_test rule and is used to trigger tests for a CEL
    checked expression.

    Args:
      name: str name for the generated artifact
      test_suite: str label of a file containing a cel.expr.conformance.test.TestSuite message in
          Textproto format. Alternatively, the file can also contain a test.Suite object in YAML format.
      config: str label of a file containing a google.api.expr.conformance.Environment message in
          Textproto format. Alternatively, the file can also contain the environment configuration represented
          as an env.Config object in YAML format.
      base_config: str label of a file containing a google.api.expr.conformance.Environment message
          in Textproto format. Alternatively, the file can also contain the environment configuration represented
          as an env.Config object in YAML format.
      test_src: source file containing the invocaton of the test runner.
      cel_expr: str label of a file containing either a CEL policy or a CEL expression or a checked
          expression or a raw CEL expression string.
      is_raw_expr: boolean indicating if the cel_expr is a raw CEL expression string.
      test_data_path: path of the directory containing the test files. This is needed only
          if the test files are not located in the same directory as the BUILD file.
      file_descriptor_set: str label or filename pointing to a file_descriptor_set message. Note:
          this must be in binary format with either a .binarypb or .pb or.fds extension. If you need
          to support a textformat file_descriptor_set, embed it in the environment file. (default None)
      filegroup: str label of a filegroup containing the test suite, config, and cel_expr.
      enable_coverage: boolean indicating if coverage should be enabled for the test.
      deps: list of dependencies for the go_test rule
      data: list of data dependencies for the go_test rule
      **kwargs: additional arguments to pass to the go_test rule
    """

    _, cel_expr_format = paths.split_extension(cel_expr)
    if filegroup != "":
        data = data + [filegroup]
    elif test_data_path != "" and test_data_path != native.package_name():
        if config != "":
            data = data + [test_data_path + ":" + config]
        if base_config != "":
            data = data + [test_data_path + ":" + base_config]
        if test_suite != "":
            data = data + [test_data_path + ":" + test_suite]
        if cel_expr_format == ".cel" or cel_expr_format == ".celpolicy" or cel_expr_format == ".yaml":
            data = data + [test_data_path + ":" + cel_expr]
    else:
        test_data_path = native.package_name()
        if config != "":
            data = data + [config]
        if base_config != "":
            data = data + [base_config]
        if test_suite != "":
            data = data + [test_suite]
        if cel_expr_format == ".cel" or cel_expr_format == ".celpolicy" or cel_expr_format == ".yaml":
            data = data + [cel_expr]

    test_data_path = test_data_path.lstrip("/")

    if test_suite != "":
        test_suite = test_data_path + "/" + test_suite

    if config != "":
        config = test_data_path + "/" + config

    if base_config != "":
        base_config = test_data_path + "/" + base_config

    srcs = [test_src]

    args = [
        "--test_suite_path=%s" % test_suite,
        "--config_path=%s" % config,
        "--base_config_path=%s" % base_config,
        "--enable_coverage=%s" % enable_coverage,
    ]

    if cel_expr_format == ".cel" or cel_expr_format == ".celpolicy" or cel_expr_format == ".yaml":
        args.append("--cel_expr=%s" % test_data_path + "/" + cel_expr)
    elif is_raw_expr:
        data = data + [cel_expr]
        args.append("--cel_expr=%s" % cel_expr)
    else:
        args.append("--cel_expr=$(location {})".format(cel_expr))

    if file_descriptor_set != "":
        data = data + [file_descriptor_set]
        args.append("--file_descriptor_set=$(location {})".format(file_descriptor_set))

    go_test(
        name = name,
        srcs = srcs,
        args = args,
        data = data,
        deps = deps + [
            "//cel:go_default_library",
            "//common/types:go_default_library",
            "//common/types/ref:go_default_library",
            "//interpreter:go_default_library",
            "//test:go_default_library",
            "//tools/celtest:go_default_library",
            "//tools/compiler:go_default_library",
            "@com_github_google_go_cmp//cmp:go_default_library",
            "@dev_cel_expr//:expr",
            "@dev_cel_expr//conformance/test:go_default_library",
            "@in_yaml_go_yaml_v3//:go_default_library",
            "@io_bazel_rules_go//go/runfiles",
            "@org_golang_google_genproto_googleapis_api//expr/v1alpha1:go_default_library",
            "@org_golang_google_protobuf//encoding/prototext:go_default_library",
            "@org_golang_google_protobuf//proto:go_default_library",
            "@org_golang_google_protobuf//reflect/protodesc:go_default_library",
            "@org_golang_google_protobuf//reflect/protoreflect:go_default_library",
            "@org_golang_google_protobuf//reflect/protoregistry:go_default_library",
            "@org_golang_google_protobuf//testing/protocmp:go_default_library",
            "@org_golang_google_protobuf//types/descriptorpb:go_default_library",
            "@org_golang_google_protobuf//types/dynamicpb:go_default_library",
        ],
        **kwargs
    )
