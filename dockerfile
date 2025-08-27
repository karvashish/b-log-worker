FROM golang:1.25-alpine AS build
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go build -ldflags="-s -w" -o /out/b-log-worker .

FROM alpine:3.20
WORKDIR /app
RUN adduser -D -H -u 10001 appuser && apk add --no-cache ca-certificates
COPY --from=build /out/b-log-worker /app/b-log-worker
RUN mkdir -p /app/tmp && chown -R appuser:appuser /app /app/tmp
USER appuser
ENTRYPOINT ["/app/b-log-worker"]
