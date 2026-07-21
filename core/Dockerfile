# Build context is ./core (see docker-compose.yml). Hash-pinned workflow
# templates are mounted at runtime (WORKFLOWS_DIR), not baked in.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# pgx/pgx is pure Go — a static binary runs on a minimal base.
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.20
# ca-certificates: the tag DB's live Danbooru check is HTTPS.
RUN apk add --no-cache ca-certificates \
 && adduser -D -u 10001 app \
 && mkdir -p /data && chown app /data
COPY --from=build /out/server /usr/local/bin/server
USER app
ENV DATA_DIR=/data WORKFLOWS_DIR=/workflows PORT=8080
EXPOSE 8080
ENTRYPOINT ["server"]
