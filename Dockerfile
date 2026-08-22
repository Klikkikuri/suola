# NOTICE: When updating base images, make sure they use the same base image (i.e. debian bookworm)
ARG GO_VERSION=1.26

# TinyGo, used to build the smaller Wasm modules.
ARG TINYGO_VERSION=0.41.1

# WASI runtime, used to run the wasip1 test binary (make test-wasi).
ARG WASMTIME_VERSION=48.0.0

# Python interface
ARG UV_VERSION=0.5.20
ARG UV_PROJECT_ENVIRONMENT=/app/python/.venv/
ARG PYTHON_VERSION=3.11

FROM ghcr.io/tinygo-org/tinygo:${TINYGO_VERSION} AS tinygo


##
## WASI runtime stage
## ==================
## Source for the wasmtime binary; wasmtime publishes no image. Needed by
## `tinygo test -target=wasip1`, whose target spec invokes `wasmtime run`.
##
## The binary is dynamically linked but requires no more than GLIBC_2.28, which
## both bookworm (2.36) and trixie (2.41) satisfy, so the stage base does not
## have to match the stages that COPY it.
FROM debian:bookworm-slim AS wasmtime

ARG WASMTIME_VERSION

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        # The release tarball is xz-compressed
        xz-utils

# uname -m already spells the architecture the way the release assets do
# (x86_64 / aarch64), so no mapping is needed.
RUN ARCH="$(uname -m)"; \
    RELEASE="wasmtime-v${WASMTIME_VERSION}-${ARCH}-linux"; \
    curl -fsSL -o /tmp/wasmtime.tar.xz \
        "https://github.com/bytecodealliance/wasmtime/releases/download/v${WASMTIME_VERSION}/${RELEASE}.tar.xz" && \
    tar -xJf /tmp/wasmtime.tar.xz -C /tmp && \
    mv "/tmp/${RELEASE}/wasmtime" /usr/local/bin/wasmtime && \
    rm -rf /tmp/wasmtime.tar.xz "/tmp/${RELEASE}" && \
    wasmtime --version


##
## Builder stage
## =============
FROM golang:${GO_VERSION} AS wasm-builder

# Create and change to the app directory.
WORKDIR /app

# TinyGo drives the Go toolchain already present in this image.
COPY --from=tinygo /usr/local/tinygo /usr/local/tinygo
RUN ln -s ../tinygo/bin/tinygo /usr/local/bin/tinygo && \
    tinygo version

# The Makefile falls back to stock Go when TinyGo is missing, which is a
# convenience for local work only. Every image built here has TinyGo, so pin
# it: a fallback in CI or a release build would silently ship the much larger
# stock-Go modules, and this turns that into a build failure instead.
ENV TINYGO=tinygo

RUN --mount=type=bind,source=go.mod,target=go.mod \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    go mod download

# Copy local code to the container image.
COPY . .

# Build the binary.

# To be considered; Should we add the rules.yaml file a remote repo?
# ADD git@github.com:Klikkikuri/rahti.git:rules.yaml /app/rules.yaml

CMD ["/bin/bash", "-c", "make build-wasm"]


## Test stage
## ==========
FROM wasm-builder AS test

# Node loads build/js.wasm for the browser-module smoke tests (make test-js).
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends nodejs

# Runs the wasip1 test binary for `make test-wasi`.
COPY --from=wasmtime /usr/local/bin/wasmtime /usr/local/bin/wasmtime

CMD ["/bin/bash", "-c", "make test test-wasi test-js"]


## Python stage
## ============
FROM ghcr.io/astral-sh/uv:${UV_VERSION} AS uv
FROM debian:bookworm-slim AS python-builder

ARG UV_VERSION \
    UV_PROJECT_ENVIRONMENT \
    PYTHON_VERSION

ENV UV_PROJECT_ENVIRONMENT=${UV_PROJECT_ENVIRONMENT} \
    UV_PYTHON_VERSION=${PYTHON_VERSION} \
    UV_CACHE_DIR=/tmp/uv-cache \
    UV_LINK_MODE=copy \
    UV_COMPILE_BYTECODE=1

WORKDIR /app

# Install python $PYTHON_VERSION
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        make \
        git \
        python${PYTHON_VERSION} \
        python${PYTHON_VERSION}-venv

VOLUME ["${UV_PROJECT_ENVIRONMENT}"]
COPY --from=uv /uv /uvx /usr/local/bin/
RUN mkdir -p "${UV_PROJECT_ENVIRONMENT}"
# WORKDIR /app/python

RUN --mount=type=cache,target=/tmp/uv-cache \
    --mount=type=bind,source=python/uv.lock,target=python/uv.lock \
    --mount=type=bind,source=python/pyproject.toml,target=python/pyproject.toml \
    uv venv \
        --directory /app/python \
        --python "/usr/bin/python${PYTHON_VERSION}" \
        "${UV_PROJECT_ENVIRONMENT}"

COPY . .

# Copy build objects
COPY --from=wasm-builder /app/build /app/build

CMD ["/bin/bash", "-c", "make build-python"]


## Python Test stage
## =================
FROM python-builder AS python-test

ARG UV_PROJECT_ENVIRONMENT \
    PYTHON_VERSION

ENV UV_PROJECT_ENVIRONMENT=${UV_PROJECT_ENVIRONMENT} \
    PATH="${UV_PROJECT_ENVIRONMENT}/bin:${PATH}"

COPY . .

RUN --mount=type=cache,target=/tmp/uv-cache \
    uv sync \
        --directory /app/python \
        --group test

CMD ["/bin/bash", "-c", "uv run --directory /app/python pytest -v"]


## Development stage
## =================
FROM mcr.microsoft.com/devcontainers/go:2-${GO_VERSION}-bookworm AS devcontainer

ARG UV_VERSION \
    UV_PROJECT_ENVIRONMENT \
    PYTHON_VERSION

# /app folder is mounted as a volume
ENV UV_LINK_MODE=copy \
    UV_CACHE_DIR=/tmp/uv-cache

# Create and change to the app directory.
WORKDIR /app

COPY --from=wasm-builder --chown=1000:1000 /go /go
COPY --from=wasm-builder --chown=1000:1000 /app /app

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        python${PYTHON_VERSION} \
        wabt \
        # Loads build/js.wasm for `make test-js`
        nodejs \
        # Bookworm support file for TinyGo
        libstdc++6 \
        # tools
        ripgrep \
        fd-find \
        jq yq \
    # Link fd-find to fd, so that coding agents can find it in the PATH
    && ln -sf "$(command -v fdfind)" /usr/local/bin/fd

COPY --from=tinygo /usr/local/tinygo /usr/local/tinygo
RUN ln -s ../tinygo/bin/tinygo /usr/local/bin/tinygo && \
    tinygo version

# Runs the wasip1 test binary for `make test-wasi`, so every make test* target
# works in the devcontainer, not just in CI.
COPY --from=wasmtime /usr/local/bin/wasmtime /usr/local/bin/wasmtime
RUN wasmtime --version

COPY --from=uv /uv /uvx /usr/local/bin/
RUN echo 'eval "$(uv generate-shell-completion bash)"' >> /etc/bash.bashrc

# Copy the Python virtual environment from builder stage
VOLUME ["${UV_PROJECT_ENVIRONMENT}"]
COPY --from=python-builder --chown=vscode:vscode  "${UV_PROJECT_ENVIRONMENT}" "${UV_PROJECT_ENVIRONMENT}"

USER vscode

RUN  --mount=type=cache,target=/tmp/uv-cache,uid=1000,gid=1000 \
    uv sync \
        --verbose \
        --directory /app/python \
        --python "/usr/bin/python${PYTHON_VERSION}" \
        --compile-bytecode

ENV PATH="${UV_PROJECT_ENVIRONMENT}/bin:${PATH}"

CMD ["/bin/bash"]
