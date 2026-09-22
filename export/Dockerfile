# Matches terraform/modules/export-task's Fargate task definition: ARM64, Linux.
#
# Build/push with: docker build --provenance=false --sbom=false -t <repo>:latest .
# Without those two flags, newer BuildKit defaults to pushing an OCI image index (attaching
# provenance/SBOM attestations) even for this single-platform build - Fargate then fails to pull
# it with "CannotPullContainerError: ... does not contain descriptor matching platform
# 'linux/arm64 v8'", despite the underlying image genuinely being arm64. Found the hard way
# deploying cmd/trim (Phase 4) - see runbooks/postgres-partitioning.md's rehearsal log.
FROM --platform=linux/arm64 golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /export ./cmd/export
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /curate ./cmd/curate
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /compact ./cmd/compact
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /erasure ./cmd/erasure
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /trim ./cmd/trim

FROM --platform=linux/arm64 gcr.io/distroless/static-debian12
COPY --from=build /export /export
COPY --from=build /curate /curate
COPY --from=build /compact /compact
COPY --from=build /erasure /erasure
COPY --from=build /trim /trim
COPY internal/config/tables.yaml /etc/mtsai-datalake-export/tables.yaml
# /curate, /compact, /erasure, and /trim are each selected by their own ECS task definition's
# entryPoint override - the image's default (this ENTRYPOINT) stays /export so the existing
# nightly export task is unaffected.
ENTRYPOINT ["/export"]
