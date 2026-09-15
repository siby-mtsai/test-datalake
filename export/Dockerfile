# Matches terraform/modules/export-task's Fargate task definition: ARM64, Linux.
FROM --platform=linux/arm64 golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /export ./cmd/export

FROM --platform=linux/arm64 gcr.io/distroless/static-debian12
COPY --from=build /export /export
COPY internal/config/tables.yaml /etc/mtsai-datalake-export/tables.yaml
ENTRYPOINT ["/export"]
