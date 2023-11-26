# build app
FROM --platform=$BUILDPLATFORM golang:1.20-alpine3.18 AS app-builder

ARG VERSION=dev
ARG REVISION=dev
ARG BUILDTIME
ARG TARGETOS TARGETARCH

RUN apk add --no-cache git tzdata

ENV SERVICE=seasonpackarr

WORKDIR /src
COPY . ./

RUN --mount=target=. \
    go mod download


RUN --mount=target=. \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${REVISION} -X main.date=${BUILDTIME}" -o /out/bin/seasonpackarr cmd/seasonpackarr/main.go

# build runner
FROM alpine:latest

LABEL org.opencontainers.image.source = "https://github.com/nuxencs/seasonpackarr"

ENV HOME="/config" \
    XDG_CONFIG_HOME="/config" \
    XDG_DATA_HOME="/config"

RUN apk add --no-cache ca-certificates curl tzdata jq

WORKDIR /app

VOLUME /config

COPY --from=app-builder /out/bin/seasonpackarr /usr/bin/

EXPOSE 42069

ENTRYPOINT ["/usr/bin/seasonpackarr", "start", "--config", "/config"]
