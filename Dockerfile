# build app
FROM golang:1.22-alpine3.19 AS app-builder

WORKDIR /src

ENV SERVICE=seasonpackarr
ARG VERSION=dev \
    REVISION=dev \
    BUILDTIME \

COPY go.mod go.sum ./
RUN go mod download
COPY . ./

RUN --network=none \
    go build -ldflags "-s -w -X seasonpackarr/internal/buildinfo.Version=${VERSION} -X seasonpackarr/internal/buildinfo.Commit=${REVISION} -X seasonpackarr/internal/buildinfo.Date=${BUILDTIME}" -o bin/seasonpackarr main.go

# build runner
FROM alpine:latest
RUN apk add --no-cache ca-certificates curl tzdata jq

LABEL org.opencontainers.image.source="https://github.com/nuxencs/seasonpackarr" \
      org.opencontainers.image.licenses="GPL-2.0-or-later" \
      org.opencontainers.image.base.name="alpine:latest"

ENV HOME="/config" \
    XDG_CONFIG_HOME="/config" \
    XDG_DATA_HOME="/config"

WORKDIR /app
VOLUME /config
EXPOSE 42069

COPY --from=app-builder /src/bin/seasonpackarr /usr/bin/

ENTRYPOINT ["/usr/bin/seasonpackarr", "start", "--config", "/config"]