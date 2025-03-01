# Tinygo container for building WebAssembly binaries
ARG TINYGO_VERSION=0.35

# Use the official golang image to create a build artifact.
FROM tinygo/tinygo:${TINYGO_VERSION} AS builder

# Create and change to the app directory.
WORKDIR /app

# Copy local code to the container image.
COPY . .

# Build the binary.
RUN tinygo build -o main.wasm -target=wasi .

# Devcontainer
FROM mcr.microsoft.com/devcontainers/go:1-1.23-bookworm as devcontainer

# # Install TinyGo
# RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
#     --mount=type=cache,target=/var/lib/apt,sharing=locked \
#     apt-get update && apt-get --no-install-recommends install -y \

# Create and change to the app directory.
WORKDIR /app

COPY . .

# To be considered; Should we add the rules.yaml file a remote repo?
# ADD git@github.com:Klikkikuri/rahti.git:rules.yaml /app/rules.yaml
