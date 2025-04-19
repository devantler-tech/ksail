# Disclaimer! This Dockerfile is badly maintained. Contributions are welcome to make running ksail as a container easier.
FROM ubuntu:24.04

ARG ARCH=arm64
ENV DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

RUN apt-get update && \
  apt-get install -y --no-install-recommends curl=7.68.0-1ubuntu2.6 jq=1.6-1ubuntu0.20.04.1 && \
  curl -fsSL https://get.docker.com -o get-docker.sh && \
  sh get-docker.sh && \
  LATEST_RELEASE=$(curl --silent "https://api.github.com/repos/devantler/ksail/releases/latest" | jq -r .tag_name) && \
  curl -L -o ksail "https://github.com/devantler/ksail/releases/download/${LATEST_RELEASE}/ksail-linux-${ARCH}" && \
  chmod +x ksail && \
  mv ksail /usr/local/bin/ && \
  apt-get clean && rm -rf /var/lib/apt/lists/*

WORKDIR /app
ENTRYPOINT [ "ksail" ]
