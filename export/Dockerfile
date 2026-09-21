# Matches terraform/modules/export-task's Fargate task definition: ARM64, Linux.
FROM --platform=linux/arm64 golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /export ./cmd/export
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /curate ./cmd/curate

FROM --platform=linux/arm64 gcr.io/distroless/static-debian12
COPY --from=build /export /export
COPY --from=build /curate /curate
COPY internal/config/tables.yaml /etc/mtsai-datalake-export/tables.yaml
# /curate is selected by the curate ECS task definition's own entryPoint override - the image's
# default (this ENTRYPOINT) stays /export so the existing nightly export task is unaffected.
ENTRYPOINT ["/export"]
