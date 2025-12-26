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
	"ksail cluster" \
	"CLI Flags Reference" \
	"" \
	"'$KSAIL_BINARY' cluster --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-connect.md" \
	"ksail cluster connect" \
	"ksail cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster connect --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-create.md" \
	"ksail cluster create" \
	"ksail cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster create --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-delete.md" \
	"ksail cluster delete" \
	"ksail cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster delete --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-info.md" \
	"ksail cluster info" \
	"ksail cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster info --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-init.md" \
	"ksail cluster init" \
	"ksail cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster init --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-list.md" \
	"ksail cluster list" \
	"ksail cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster list --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-start.md" \
	"ksail cluster start" \
	"ksail cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster start --help"

create_doc_page \
	"$DOCS_DIR/cluster/cluster-stop.md" \
	"ksail cluster stop" \
	"ksail cluster" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cluster stop --help"

echo "Generating cipher command documentation..."
mkdir -p "$DOCS_DIR/cipher"

create_doc_page \
	"$DOCS_DIR/cipher/cipher-root.md" \
	"ksail cipher" \
	"CLI Flags Reference" \
	"" \
	"'$KSAIL_BINARY' cipher --help"

create_doc_page \
	"$DOCS_DIR/cipher/cipher-decrypt.md" \
	"ksail cipher decrypt" \
	"ksail cipher" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cipher decrypt --help"

create_doc_page \
	"$DOCS_DIR/cipher/cipher-edit.md" \
	"ksail cipher edit" \
	"ksail cipher" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cipher edit --help"

create_doc_page \
	"$DOCS_DIR/cipher/cipher-encrypt.md" \
	"ksail cipher encrypt" \
	"ksail cipher" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cipher encrypt --help"

create_doc_page \
	"$DOCS_DIR/cipher/cipher-import.md" \
	"ksail cipher import" \
	"ksail cipher" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' cipher import --help"

echo "Generating workload command documentation..."
mkdir -p "$DOCS_DIR/workload"

create_doc_page \
	"$DOCS_DIR/workload/workload-root.md" \
	"ksail workload" \
	"CLI Flags Reference" \
	"" \
	"'$KSAIL_BINARY' workload --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-apply.md" \
	"ksail workload apply" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload apply --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-create.md" \
	"ksail workload create" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload create --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-delete.md" \
	"ksail workload delete" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload delete --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-describe.md" \
	"ksail workload describe" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload describe --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-edit.md" \
	"ksail workload edit" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload edit --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-exec.md" \
	"ksail workload exec" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload exec --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-explain.md" \
	"ksail workload explain" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload explain --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-expose.md" \
	"ksail workload expose" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload expose --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-get.md" \
	"ksail workload get" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload get --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-install.md" \
	"ksail workload install" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload install --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-logs.md" \
	"ksail workload logs" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload logs --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-push.md" \
	"ksail workload push" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload push --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-reconcile.md" \
	"ksail workload reconcile" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload reconcile --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-rollout.md" \
	"ksail workload rollout" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload rollout --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-scale.md" \
	"ksail workload scale" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload scale --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-validate.md" \
	"ksail workload validate" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload validate --help"

create_doc_page \
	"$DOCS_DIR/workload/workload-wait.md" \
	"ksail workload wait" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload wait --help"

echo "Generating workload gen subcommand documentation..."
mkdir -p "$DOCS_DIR/workload/gen"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-root.md" \
	"ksail workload gen" \
	"ksail workload" \
	"CLI Flags Reference" \
	"'$KSAIL_BINARY' workload gen --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-clusterrole.md" \
	"ksail workload gen clusterrole" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen clusterrole --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-clusterrolebinding.md" \
	"ksail workload gen clusterrolebinding" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen clusterrolebinding --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-configmap.md" \
	"ksail workload gen configmap" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen configmap --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-cronjob.md" \
	"ksail workload gen cronjob" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen cronjob --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-deployment.md" \
	"ksail workload gen deployment" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen deployment --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-helmrelease.md" \
	"ksail workload gen helmrelease" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen helmrelease --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-ingress.md" \
	"ksail workload gen ingress" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen ingress --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-job.md" \
	"ksail workload gen job" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen job --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-namespace.md" \
	"ksail workload gen namespace" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen namespace --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-poddisruptionbudget.md" \
	"ksail workload gen poddisruptionbudget" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen poddisruptionbudget --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-priorityclass.md" \
	"ksail workload gen priorityclass" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen priorityclass --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-quota.md" \
	"ksail workload gen quota" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen quota --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-role.md" \
	"ksail workload gen role" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen role --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-rolebinding.md" \
	"ksail workload gen rolebinding" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen rolebinding --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-secret.md" \
	"ksail workload gen secret" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen secret --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-service.md" \
	"ksail workload gen service" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen service --help"

create_doc_page \
	"$DOCS_DIR/workload/gen/gen-serviceaccount.md" \
	"ksail workload gen serviceaccount" \
	"ksail workload gen" \
	"ksail workload" \
	"'$KSAIL_BINARY' workload gen serviceaccount --help"

echo "CLI flags documentation generation completed successfully"
echo "Generated $(find "$DOCS_DIR" -name '*.md' | wc -l) documentation pages"
