
ARG GO_VERSION=1.24

# Use the official golang image to create a build artifact.
FROM --platform=${BUILDPLATFORM} golang:${GO_VERSION} AS builder

# Create and change to the app directory.
WORKDIR /app

RUN --mount=type=bind,source=go.mod,target=go.mod \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    go mod download

# Copy local code to the container image.
COPY . .

# Build the binary.

CMD ["./build.sh"]

FROM builder AS test

RUN --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    go test -v main.go

# Devcontainer
FROM mcr.microsoft.com/devcontainers/go:${GO_VERSION}-bookworm AS devcontainer

# # Install TinyGo
# RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
#     --mount=type=cache,target=/var/lib/apt,sharing=locked \
#     apt-get update && apt-get --no-install-recommends install -y \

# Create and change to the app directory.
WORKDIR /app

ENV WASMTIME_HOME=/usr/local/wasmtime

# Install wasmtime
ADD https://wasmtime.dev/install.sh /tmp/install-wasmtime.sh
RUN chmod +x /tmp/install-wasmtime.sh && \
    /tmp/install-wasmtime.sh --version v33.0.0 && \
    echo "export PATH=\$PATH:$WASMTIME_HOME/bin" >> /etc/profile && \
    rm -f /tmp/install-wasmtime.sh

USER vscode

RUN --mount=type=bind,source=go.mod,target=go.mod \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    go mod download

COPY . .

# To be considered; Should we add the rules.yaml file a remote repo?
# ADD git@github.com:Klikkikuri/rahti.git:rules.yaml /app/rules.yaml
