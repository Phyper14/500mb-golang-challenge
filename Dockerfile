# syntax=docker/dockerfile:1.7

# --- build stage -----------------------------------------------------------
# Static, CGO-disabled binary: no libc dependency, so the final image can be
# `scratch` (or `gcr.io/distroless/static`) with zero shared libraries to
# patch/scan and the smallest possible footprint/RSS baseline.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build

WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# --- runtime stage -----------------------------------------------------------
# distroless/static: no shell, no package manager, non-root by default
# (nonroot user, uid 65532) — satisfies the challenge's hardening
# requirements (read_only rootfs, non-root, no cap needed) out of the box.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

COPY --from=build /out/api /api

USER nonroot:nonroot

EXPOSE 8000

ENTRYPOINT ["/api"]
