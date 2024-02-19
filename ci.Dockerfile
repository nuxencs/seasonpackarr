# build base
FROM --platform=$BUILDPLATFORM golang:1.20-alpine3.19 AS app-base

ENV SERVICE=seasonpackarr
WORKDIR /src
ARG VERSION=dev \
    REVISION=dev \
    BUILDTIME \
    TARGETOS TARGETARCH

COPY go.mod go.sum ./
RUN go mod download
COPY . ./

# build seasonpackarr
FROM --platform=$BUILDPLATFORM app-base AS seasonpackarr

RUN --mount=target=. \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${REVISION} -X main.date=${BUILDTIME}" -o /out/bin/seasonpackarr cmd/seasonpackarr/main.go

# build runner
FROM alpine:latest as RUNNER
RUN apk add --no-cache ca-certificates curl tzdata jq

LABEL org.opencontainers.image.source = "https://github.com/nuxencs/seasonpackarr" \
      org.opencontainers.image.licenses = "GPL-2.0-or-later" \
      org.opencontainers.image.base.name = "alpine:latest"

ENV HOME="/config" \
    XDG_CONFIG_HOME="/config" \
    XDG_DATA_HOME="/config"

WORKDIR /app
VOLUME /config
EXPOSE 42069
ENTRYPOINT ["/usr/bin/seasonpackarr", "start", "--config", "/config"]

COPY --link --from=seasonpackarr /out/bin/seasonpackarr /usr/bin/