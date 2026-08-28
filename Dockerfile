ARG DEPENDENCY_DOCKER_REGISTRY=docker.io
FROM ${DEPENDENCY_DOCKER_REGISTRY}/library/golang:1.25-bookworm AS builder
ARG SERVICEGEN_RUNTIME_STRIP=ON
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN if [ "${SERVICEGEN_RUNTIME_STRIP}" = "ON" ]; then \
      GO_LINKER_FLAGS="-s -w"; \
    else \
      GO_LINKER_FLAGS=""; \
    fi \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="${GO_LINKER_FLAGS}" -o /out/orderservice ./cmd/orderservice \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="${GO_LINKER_FLAGS}" -o /out/inventoryservice ./cmd/inventoryservice

FROM ${DEPENDENCY_DOCKER_REGISTRY}/library/debian:bookworm-slim AS runtime
ARG DEPENDENCY_APT_DEBIAN_URL=
ARG DEPENDENCY_APT_DEBIAN_SECURITY_URL=
RUN if [ -n "${DEPENDENCY_APT_DEBIAN_URL}${DEPENDENCY_APT_DEBIAN_SECURITY_URL}" ]; then \
      find /etc/apt -type f \( -name '*.list' -o -name '*.sources' \) -exec sed -i \
        -e "s|http://deb.debian.org/debian-security|${DEPENDENCY_APT_DEBIAN_SECURITY_URL}|g" \
        -e "s|http://deb.debian.org/debian|${DEPENDENCY_APT_DEBIAN_URL}|g" {} +; \
    fi
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*

FROM runtime AS orderservice
COPY --from=builder /out/orderservice /usr/local/bin/orderservice
ENTRYPOINT ["orderservice"]

FROM runtime AS inventoryservice
COPY --from=builder /out/inventoryservice /usr/local/bin/inventoryservice
ENTRYPOINT ["inventoryservice"]
