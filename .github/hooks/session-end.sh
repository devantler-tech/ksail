#!/bin/bash
set -euo pipefail

echo "Running golangci-lint with --fix..."
golangci-lint run --fix --timeout 5m || true

echo "Running golangci-lint fmt..."
golangci-lint fmt || true

echo "Running jscpd..."
jscpd --config .jscpd.json . || true
