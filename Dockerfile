# NOTICE: When updating base images, make sure they use the same base image (i.e. debian bookworm)
ARG GO_VERSION=1.25

# Wasmtime installation directory
ARG WASMTIME_HOME=/usr/local/wasmtime

# Python interface
ARG UV_VERSION=0.5.20
ARG UV_PROJECT_ENVIRONMENT=/app/python/.venv/
ARG PYTHON_VERSION=3.11

##
## Builder stage
## =============
FROM --platform=${BUILDPLATFORM} golang:${GO_VERSION} AS wasm-builder

# Create and change to the app directory.
WORKDIR /app

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

ARG WASMTIME_HOME
ENV WASMTIME_HOME=$WASMTIME_HOME
ENV PATH=$PATH:${WASMTIME_HOME}/bin

# Install wasmtime for testing wasi
# Install xz-utils for extracting the wasmtime tarball
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends xz-utils

ADD https://wasmtime.dev/install.sh /tmp/install-wasmtime.sh
RUN chmod +x /tmp/install-wasmtime.sh && \
    echo "Installing wasmtime to ${WASMTIME_HOME}" && \
    /tmp/install-wasmtime.sh --version v33.0.0 && \
    echo "export PATH=\$PATH:${WASMTIME_HOME}/bin" >> /etc/profile.d/wasmtime.sh && \
    chmod +x /etc/profile.d/wasmtime.sh && \
    rm -f /tmp/install-wasmtime.sh

CMD ["/bin/bash", "-c", "make test"]

## Python stage
## ============
FROM ghcr.io/astral-sh/uv:${UV_VERSION} AS uv
FROM debian:bookworm-slim AS python-builder

ARG UV_VERSION \
    UV_PROJECT_ENVIRONMENT \
    PYTHON_VERSION

ENV UV_PROJECT_ENVIRONMENT=${UV_PROJECT_ENVIRONMENT} \
    UV_PYTHON_VERSION=${PYTHON_VERSION} \
    UV_LINK_MODE=copy \
    UV_COMPILE_BYTECODE=1

WORKDIR /app

# Install python $PYTHON_VERSION
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        make \
        python${PYTHON_VERSION} \
        python${PYTHON_VERSION}-venv

VOLUME ["${UV_PROJECT_ENVIRONMENT}"]
COPY --from=uv /uv /uvx /usr/local/bin/
RUN mkdir -p "${UV_PROJECT_ENVIRONMENT}"
# WORKDIR /app/python

RUN --mount=type=cache,target=/root/.cache/uv \
    --mount=type=bind,source=python/uv.lock,target=python/uv.lock \
    --mount=type=bind,source=python/pyproject.toml,target=python/pyproject.toml \
    uv venv \
        --directory /app/python \
        --python "/usr/bin/python${PYTHON_VERSION}" \
        "${UV_PROJECT_ENVIRONMENT}"

# Copy build objects
COPY --from=wasm-builder . .
CMD ["/bin/bash", "-c", "make build-wasm"]

## Development stage
## =================
FROM mcr.microsoft.com/devcontainers/go:2-${GO_VERSION}-bookworm AS devcontainer

ARG UV_VERSION \
    UV_PROJECT_ENVIRONMENT \
    PYTHON_VERSION

# /app folder is mounted as a volume
ENV UV_LINK_MODE=copy

# Create and change to the app directory.
WORKDIR /app

# Copy wasmtime from the test stage
ARG WASMTIME_HOME
ENV WASMTIME_HOME=${WASMTIME_HOME}
COPY --from=test ${WASMTIME_HOME} ${WASMTIME_HOME}
COPY --from=test /etc/profile.d/wasmtime.sh /etc/profile.d/wasmtime.sh

COPY --from=wasm-builder --chown=1000:1000 /go /go
COPY --from=wasm-builder --chown=1000:1000 /app /app

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        python${PYTHON_VERSION} \
        wabt

COPY --from=uv /uv /uvx /usr/local/bin/
RUN echo 'eval "$(uv generate-shell-completion bash)"' >> /etc/bash.bashrc

# Copy the Python virtual environment from builder stage
VOLUME ["${UV_PROJECT_ENVIRONMENT}"]
COPY --from=python-builder --chown=vscode:vscode  "${UV_PROJECT_ENVIRONMENT}" "${UV_PROJECT_ENVIRONMENT}"

USER vscode

RUN  --mount=type=cache,target=/root/.cache/uv \
    uv sync \
        --verbose \
        --directory /app/python \
        --python "/usr/bin/python${PYTHON_VERSION}" \
        --compile-bytecode

ENV PATH="${UV_PROJECT_ENVIRONMENT}/bin:${PATH}"

CMD ["/bin/bash"]
