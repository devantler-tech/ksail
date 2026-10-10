# Copyright 2026 Google LLC
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

"""repositories for loading non bzlmod dependencies"""

load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")

def cel_policy_dependency():
    cel_policy_tag = "e4c38defbbf34dfff2dc448dc58e93a9733ae8b1"
    cel_policy_sha = "46378e0d17a16465899f9fefc94c3d44e1f40aedd8a31c9c0b2b6198048eabd6"
    http_archive(
        name = "cel_policy",
        sha256 = cel_policy_sha,
        strip_prefix = "cel-policy-%s" % cel_policy_tag,
        url = "https://github.com/cel-expr/cel-policy/archive/%s.tar.gz" % cel_policy_tag,
    )

def _non_module_dependencies_impl(_ctx):
    cel_policy_dependency()

non_module_dependencies = module_extension(
    implementation = _non_module_dependencies_impl,
)
