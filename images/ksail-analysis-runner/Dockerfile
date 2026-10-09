FROM ghcr.io/actions/actions-runner@sha256:e5496277be5d09bc968b3d64911b74e219ac4a3f2edce956a3ecf9271bea1ef4

ARG TARGETARCH
USER 0:0
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN printf 'APT::Snapshot "20261005T000000Z";\n' > /etc/apt/apt.conf.d/99ksail-analysis-snapshot \
    && apt-get update \
    && apt-get install --yes --no-install-recommends \
       build-essential=12.10ubuntu1 pkg-config=1.8.1-2build1 \
       libgtk-4-dev=4.14.5+ds-0ubuntu0.10 \
       libwebkitgtk-6.0-dev=2.52.6-0ubuntu0.24.04.1 \
       xz-utils=5.6.1+really5.4.5-1ubuntu0.3 ca-certificates=20260601~24.04.1 \
    && rm -rf /var/lib/apt/lists/*

RUN set -eu; \
    case "${TARGETARCH}" in \
      amd64) go_arch=amd64; go_sha=42d158b4d8f7b61ac0a830567c940a86098fb7aac52e467a5ebec03ef5cc2f8d; \
             node_arch=x64; node_sha=df450af89261115ef9f9e3830c3eeb2cc9213b63c720b1af623cb5dcbe2e02de ;; \
      arm64) go_arch=arm64; go_sha=4a97373d49fcacdcf3694fea368a500b00ee3e963974f3e7514132717632f052; \
             node_arch=arm64; node_sha=a44aeb94849a299b22df10b9e622ec2f605c2183501bc40590705131de7c740f ;; \
      *) echo "Unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl --fail --silent --show-error --location --max-time 120 \
      "https://go.dev/dl/go1.26.9.linux-${go_arch}.tar.gz" --output /tmp/go.tar.gz; \
    printf '%s  /tmp/go.tar.gz\n' "${go_sha}" | sha256sum --check --strict; \
    tar -xzf /tmp/go.tar.gz -C /usr/local; \
    curl --fail --silent --show-error --location --max-time 120 \
      "https://nodejs.org/dist/v22.23.3/node-v22.23.3-linux-${node_arch}.tar.xz" --output /tmp/node.tar.xz; \
    printf '%s  /tmp/node.tar.xz\n' "${node_sha}" | sha256sum --check --strict; \
    mkdir -p /opt/node; \
    tar -xJf /tmp/node.tar.xz -C /opt/node --strip-components=1; \
    rm -f /tmp/go.tar.gz /tmp/node.tar.xz

COPY --chmod=0555 smoke.sh /usr/local/bin/ksail-analysis-smoke
ENV PATH="/usr/local/go/bin:/opt/node/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
    GOFLAGS="-tags=desktop" \
    GOMEMLIMIT="8GiB" \
    CGO_ENABLED="1"
USER 1001:1001
