ARG DEPENDENCY_DOCKER_REGISTRY=docker.io
FROM ${DEPENDENCY_DOCKER_REGISTRY}/library/golang:1.25-bookworm AS builder
ARG TARGETARCH
ARG SERVICEGEN_RUNTIME_STRIP=ON
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,id=servicegen-go-native-mod-v1-${TARGETARCH},target=/go/pkg/mod,sharing=locked \
    go mod download
COPY . .
RUN --mount=type=cache,id=servicegen-go-native-mod-v1-${TARGETARCH},target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=servicegen-go-native-build-v1-${TARGETARCH},target=/root/.cache/go-build,sharing=locked \
    if [ "${SERVICEGEN_RUNTIME_STRIP}" = "ON" ]; then \
      GO_LINKER_FLAGS="-s -w"; \
    else \
      GO_LINKER_FLAGS=""; \
    fi \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="${GO_LINKER_FLAGS}" -o /out/orderservice ./cmd/orderservice \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="${GO_LINKER_FLAGS}" -o /out/inventoryservice ./cmd/inventoryservice

FROM ${DEPENDENCY_DOCKER_REGISTRY}/library/debian:bookworm-slim AS runtime
ARG TARGETARCH
ARG DEPENDENCY_APT_DEBIAN_URL=
ARG DEPENDENCY_APT_DEBIAN_SECURITY_URL=
RUN rm -f /etc/apt/apt.conf.d/docker-clean
RUN --mount=type=cache,id=servicegen-apt-lists-${TARGETARCH},target=/var/lib/apt/lists,sharing=locked \
    --mount=type=cache,id=servicegen-apt-cache-${TARGETARCH},target=/var/cache/apt,sharing=locked \
    if [ -n "${DEPENDENCY_APT_DEBIAN_URL}${DEPENDENCY_APT_DEBIAN_SECURITY_URL}" ]; then \
      find /etc/apt -type f \( -name '*.list' -o -name '*.sources' \) -exec sed -i \
        -e "s|http://deb.debian.org/debian-security|${DEPENDENCY_APT_DEBIAN_SECURITY_URL}|g" \
        -e "s|http://deb.debian.org/debian|${DEPENDENCY_APT_DEBIAN_URL}|g" {} +; \
    fi
RUN --mount=type=cache,id=servicegen-apt-lists-${TARGETARCH},target=/var/lib/apt/lists,sharing=locked \
    --mount=type=cache,id=servicegen-apt-cache-${TARGETARCH},target=/var/cache/apt,sharing=locked \
    apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates

FROM runtime AS orderservice
COPY --from=builder /out/orderservice /usr/local/bin/orderservice
ENTRYPOINT ["orderservice"]

FROM runtime AS inventoryservice
COPY --from=builder /out/inventoryservice /usr/local/bin/inventoryservice
ENTRYPOINT ["inventoryservice"]
