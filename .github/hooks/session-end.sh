#!/bin/bash
set -euo pipefail

echo "Running golangci-lint with --fix..."
golangci-lint run --fix || true

echo "Running golangci-lint fmt..."
golangci-lint fmt || true

echo "Running jscpd..."
jscpd . || true
