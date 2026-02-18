# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS builder
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY web ./web

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/server ./cmd/server

FROM alpine:3.20
WORKDIR /app

COPY --from=builder /out/server /app/server
COPY --from=builder /src/web /app/web

EXPOSE 8080
CMD ["/app/server"]
