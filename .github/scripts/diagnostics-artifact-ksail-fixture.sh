#!/usr/bin/env bash
# The native artifact probe has no cluster; make captured command output unmistakably synthetic.
set -euo pipefail
printf 'KSail diagnostics artifact fixture: no cluster or cloud operation\n'
