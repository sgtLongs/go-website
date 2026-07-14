# syntax=docker/dockerfile:1

FROM golang:1.26.5-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY frontend ./frontend
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.23

RUN addgroup -S app && adduser -S -G app app

WORKDIR /app
COPY --from=build /out/server /app/server

RUN mkdir -p /app/data && chown -R app:app /app

USER app

ENV ADDRESS=:8080 \
    DATA_PATH=/app/data/game.db \
    GIN_MODE=release

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/health >/dev/null || exit 1

ENTRYPOINT ["/app/server"]
