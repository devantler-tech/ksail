#!/usr/bin/env bash

set -euo pipefail

docker_prune="${DOCKER_PRUNE:-false}"
before=$(df --output=avail -BG / | tail -1 | tr -dc '0-9')

# Largest offenders on ubuntu-latest (sizes from
# https://github.com/actions/runner-images). Order by descending size.
sudo rm -rf \
	/usr/local/lib/android \
	/usr/share/dotnet \
	/opt/ghc \
	/usr/local/share/boost \
	/usr/share/swift \
	/opt/hostedtoolcache/CodeQL \
	/opt/hostedtoolcache/PyPy \
	/opt/hostedtoolcache/Ruby \
	/opt/hostedtoolcache/Python \
	/usr/local/share/chromium \
	/usr/local/share/powershell \
	/usr/local/julia* \
	/usr/local/aws-cli \
	/usr/local/aws-sam-cli \
	/usr/share/gradle* \
	/usr/share/az* \
	/usr/share/miniconda || true

if [ "$docker_prune" = "true" ]; then
	docker system prune -af --volumes || true
fi

after=$(df --output=avail -BG / | tail -1 | tr -dc '0-9')
echo "Free disk space on /: ${before}G -> ${after}G (gained $((after - before))G)"
