#!/usr/bin/env bash
# list-link-check-files.sh — print the tracked files the link checker reads, one per line.
#
# The pull-request link check runs inside MegaLinter, which picks the files from
# .mega-linter.yml. The daily strict check in maintenance.yaml runs lychee directly and
# needs the same set, so the two checks cannot disagree about which links exist. The three
# patterns below mirror lychee's file extensions in MegaLinter and the EXCLUDED_DIRECTORIES,
# FILTER_REGEX_EXCLUDE and SPELL_LYCHEE_FILTER_REGEX_EXCLUDE settings; change them together.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

git ls-files -z |
	grep -zE '\.(md|mdx|markdown|html|htm|rst|txt|json|jsonc|json5|yaml|yml)$' |
	grep -zvE '^(megalinter-reports/|report/|\.jscpd/|schemas/|\.agents/|web/ui/|third_party/(cel-go|glamour|go-macholibre|kyverno-jmespath|jmespath|ansi|ansi-runtime|redisotel|rediscmd|dynamiclistener)/)' |
	grep -zvE '(AGENTS\.md|.*\.mdx|docs/src/.*\.md)' |
	tr '\0' '\n'
