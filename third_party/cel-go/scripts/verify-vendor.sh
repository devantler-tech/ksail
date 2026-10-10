#!/usr/bin/env bash

# Copyright 2023 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

tmp="$(mktemp -d)"
go mod vendor -o "$tmp"
if ! _out="$(diff -Naupr vendor $tmp)"; then
  echo "Verify vendor failed" >&2
  echo "If you're seeing this locally, run the below command to fix your go.mod:" >&2
  echo "go mod vendor" >&2
  exit 1
fi
