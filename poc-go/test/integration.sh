#!/bin/bash

# Integration test for KSail Go POC
set -e

echo "🧪 Running KSail Go POC Integration Tests"
echo "=========================================="

# Build the binary
echo "📦 Building KSail POC..."
go build -o ksail-poc

# Test 1: Help command
echo "🔍 Testing help command..."
./ksail-poc --help > /dev/null
echo "✅ Help command works"

# Test 2: Init with default options
echo "🔍 Testing init with defaults..."
TEST_DIR="/tmp/ksail-integration-test-default"
rm -rf "$TEST_DIR"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

/home/runner/work/ksail/ksail/poc-go/ksail-poc init > /dev/null

# Verify files were created
if [[ ! -f "ksail.yaml" ]]; then
    echo "❌ ksail.yaml not created"
    exit 1
fi

if [[ ! -f "kind.yaml" ]]; then
    echo "❌ kind.yaml not created"
    exit 1
fi

if [[ ! -f "k8s/kustomization.yaml" ]]; then
    echo "❌ k8s/kustomization.yaml not created"
    exit 1
fi

echo "✅ Init with defaults works"

# Test 3: Init with custom options
echo "🔍 Testing init with custom options..."
TEST_DIR="/tmp/ksail-integration-test-custom"
rm -rf "$TEST_DIR"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

/home/runner/work/ksail/ksail/poc-go/ksail-poc init \
    --name custom-cluster \
    --distribution K3d \
    --deployment-tool Flux \
    --cni Cilium \
    --secret-manager SOPS > /dev/null

# Verify custom configuration was applied
if ! grep -q "name: custom-cluster" ksail.yaml; then
    echo "❌ Custom cluster name not set"
    exit 1
fi

if ! grep -q "distribution: K3d" ksail.yaml; then
    echo "❌ Custom distribution not set"
    exit 1
fi

if ! grep -q "deploymentTool: Flux" ksail.yaml; then
    echo "❌ Custom deployment tool not set"
    exit 1
fi

if ! grep -q "cni: Cilium" ksail.yaml; then
    echo "❌ Custom CNI not set"
    exit 1
fi

if [[ ! -f "k3d.yaml" ]]; then
    echo "❌ k3d.yaml not created for K3d distribution"
    exit 1
fi

if [[ ! -f ".sops.yaml" ]]; then
    echo "❌ .sops.yaml not created when SOPS enabled"
    exit 1
fi

echo "✅ Init with custom options works"

# Test 4: Other commands (smoke tests)
echo "🔍 Testing other commands..."
cd "$TEST_DIR"

/home/runner/work/ksail/ksail/poc-go/ksail-poc status > /dev/null
echo "✅ Status command works"

/home/runner/work/ksail/ksail/poc-go/ksail-poc list > /dev/null
echo "✅ List command works"

/home/runner/work/ksail/ksail/poc-go/ksail-poc validate > /dev/null
echo "✅ Validate command works"

echo ""
echo "🎉 All integration tests passed!"
echo "✨ KSail Go POC is working correctly"

# Cleanup
rm -rf "/tmp/ksail-integration-test-default"
rm -rf "/tmp/ksail-integration-test-custom"