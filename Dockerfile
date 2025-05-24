
ARG GO_VERSION=1.24
ARG WASMTIME_HOME=/usr/local/wasmtime

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

# To be considered; Should we add the rules.yaml file a remote repo?
# ADD git@github.com:Klikkikuri/rahti.git:rules.yaml /app/rules.yaml

CMD ["make", "build"]

## Test stage
## ==========
FROM builder AS test

ARG WASMTIME_HOME
ENV WASMTIME_HOME=$WASMTIME_HOME

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

CMD ["make", "test"]

## Development stage
## =================
FROM mcr.microsoft.com/devcontainers/go:${GO_VERSION} AS devcontainer

# Create and change to the app directory.
WORKDIR /app

# Copy wasmtime from the test stage
ARG WASMTIME_HOME
ENV WASMTIME_HOME=${WASMTIME_HOME}
COPY --from=test ${WASMTIME_HOME} ${WASMTIME_HOME}
COPY --from=test /etc/profile.d/wasmtime.sh /etc/profile.d/wasmtime.sh

COPY --from=builder --chown=1000:1000 /go /go
COPY --from=builder --chown=1000:1000 /app /app

USER vscode

CMD ["/bin/bash"]
