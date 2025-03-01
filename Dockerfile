
ARG GO_VERSION=1.23

# Use the official golang image to create a build artifact.
FROM --platform=${BUILDPLATFORM} golang:${GO_VERSION} as builder

ENV GOOS=wasip1 \
    GOARCH=wasm

# Create and change to the app directory.
WORKDIR /app

RUN --mount=type=bind,source=go.mod,target=go.mod \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    go mod download

# Copy local code to the container image.
COPY . .

# Build the binary.
RUN --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    go build -o /app/suola.wasm main.go 

FROM builder AS test

RUN --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    go test -v main.go

# Devcontainer
FROM mcr.microsoft.com/devcontainers/go:${GO_VERSION}-bookworm as devcontainer

# # Install TinyGo
# RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
#     --mount=type=cache,target=/var/lib/apt,sharing=locked \
#     apt-get update && apt-get --no-install-recommends install -y \

# Create and change to the app directory.
WORKDIR /app

RUN --mount=type=bind,source=go.mod,target=go.mod \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    go mod download

COPY . .

# To be considered; Should we add the rules.yaml file a remote repo?
# ADD git@github.com:Klikkikuri/rahti.git:rules.yaml /app/rules.yaml
