#!/bin/bash

# Script to generate CLI flags documentation from KSail help output
# This script is used by the CI/CD pipeline to keep docs in sync

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
KSAIL_BINARY="$REPO_ROOT/ksail"
DOCS_DIR="$REPO_ROOT/docs/cli-flags"

echo "Generating CLI flags documentation from KSail help output..."

# Ensure KSail binary exists
if [ ! -f "$KSAIL_BINARY" ]; then
	echo "Error: KSail binary not found at $KSAIL_BINARY. Build it first with 'go build -o ksail'" >&2
	exit 1
fi

# Clean and recreate docs directory
rm -rf "$DOCS_DIR"
mkdir -p "$DOCS_DIR"

# Helper function to create a documentation page
# Args: $1 = output file path, $2 = title, $3 = parent, $4 = grand_parent, $5 = command to run
create_doc_page() {
	local output_file="$1"
	local title="$2"
	local parent="$3"
	local grand_parent="$4"
	local command="$5"
	
	mkdir -p "$(dirname "$output_file")"
	
	{
		echo "---"
		echo "title: \"$title\""
		if [ -n "$parent" ]; then
			echo "parent: \"$parent\""
		fi
		if [ -n "$grand_parent" ]; then
			echo "grand_parent: \"$grand_parent\""
		fi
		echo "---"
		echo ""
		echo "# $title"
		echo ""
		echo '```text'
		eval "$command" 2>&1
		echo '```'
	} > "$output_file"
}

echo "Generating root documentation..."
create_doc_page \
	"$DOCS_DIR/root.md" \
	"CLI Flags Reference" \
	"" \
	"" \
	"'$KSAIL_BINARY' --help"

echo "Generating cluster command documentation..."
mkdir -p "$DOCS_DIR/cluster"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-root.md" \
	"cluster" \
	"CLI Flags Reference" \
	"" \
	"'$KSAIL_BINARY' cluster --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-connect.md" \
	"cluster connect" \
	"cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster connect --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-create.md" \
	"cluster create" \
	"cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster create --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-delete.md" \
	"cluster delete" \
	"cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster delete --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-info.md" \
	"cluster info" \
	"cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster info --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-init.md" \
	"cluster init" \
	"cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster init --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-list.md" \
	"cluster list" \
	"cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster list --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-start.md" \
	"cluster start" \
	"cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster start --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-stop.md" \
	"cluster stop" \
	"cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster stop --help"

echo "Generating cipher command documentation..."
mkdir -p "$DOCS_DIR/cipher"

create_doc_page \
	"$DOCS_DIR/cipher/cipher-root.md" \
	"cipher" \
	"CLI Flags Reference" \
	"" \
	"'$KSAIL_BINARY' cipher --help"

create_doc_page \
	"$DOCS_DIR/cipher/cipher-decrypt.md" \
	"cipher decrypt" \
	"cipher" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cipher decrypt --help"

create_doc_page \
	"$DOCS_DIR/cipher/cipher-edit.md" \
	"cipher edit" \
	"cipher" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cipher edit --help"

create_doc_page \
	"$DOCS_DIR/cipher/cipher-encrypt.md" \
	"cipher encrypt" \
	"cipher" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cipher encrypt --help"

create_doc_page \
	"$DOCS_DIR/cipher/cipher-import.md" \
	"cipher import" \
	"cipher" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cipher import --help"

echo "Generating workload command documentation..."
mkdir -p "$DOCS_DIR/workload"

create_doc_page \
	"$DOCS_DIR/workload/workload-root.md" \
	"workload" \
	"CLI Flags Reference" \
	"" \
	"'$KSAIL_BINARY' workload --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-apply.md" \
	"workload apply" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload apply --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-create.md" \
	"workload create" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload create --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-delete.md" \
	"workload delete" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload delete --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-describe.md" \
	"workload describe" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload describe --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-edit.md" \
	"workload edit" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload edit --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-exec.md" \
	"workload exec" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload exec --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-explain.md" \
	"workload explain" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload explain --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-expose.md" \
	"workload expose" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload expose --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-get.md" \
	"workload get" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload get --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-install.md" \
	"workload install" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload install --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-logs.md" \
	"workload logs" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload logs --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-push.md" \
	"workload push" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload push --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-reconcile.md" \
	"workload reconcile" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload reconcile --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-rollout.md" \
	"workload rollout" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload rollout --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-scale.md" \
	"workload scale" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload scale --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-validate.md" \
	"workload validate" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload validate --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-wait.md" \
	"workload wait" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload wait --help"

echo "Generating workload gen subcommand documentation..."
mkdir -p "$DOCS_DIR/workload/gen"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-root.md" \
	"gen" \
	"workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload gen --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-clusterrole.md" \
	"gen clusterrole" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen clusterrole --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-clusterrolebinding.md" \
	"gen clusterrolebinding" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen clusterrolebinding --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-configmap.md" \
	"gen configmap" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen configmap --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-cronjob.md" \
	"gen cronjob" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen cronjob --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-deployment.md" \
	"gen deployment" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen deployment --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-helmrelease.md" \
	"gen helmrelease" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen helmrelease --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-ingress.md" \
	"gen ingress" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen ingress --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-job.md" \
	"gen job" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen job --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-namespace.md" \
	"gen namespace" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen namespace --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-poddisruptionbudget.md" \
	"gen poddisruptionbudget" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen poddisruptionbudget --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-priorityclass.md" \
	"gen priorityclass" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen priorityclass --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-quota.md" \
	"gen quota" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen quota --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-role.md" \
	"gen role" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen role --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-rolebinding.md" \
	"gen rolebinding" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen rolebinding --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-secret.md" \
	"gen secret" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen secret --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-service.md" \
	"gen service" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen service --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-serviceaccount.md" \
	"gen serviceaccount" \
	"gen" \
	"workload" \
	"'$KSAIL_BINARY' workload gen serviceaccount --help"

echo "CLI flags documentation generation completed successfully"
echo "Generated $(find "$DOCS_DIR" -name '*.md' | wc -l) documentation pages"
