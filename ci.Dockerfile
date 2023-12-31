# build app
FROM --platform=$BUILDPLATFORM golang:1.20-alpine3.19 AS app-builder

ARG VERSION=dev
ARG REVISION=dev
ARG BUILDTIME
ARG TARGETOS TARGETARCH

RUN apk add --no-cache git tzdata

ENV SERVICE=seasonpackarr

WORKDIR /src

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN --mount=target=. \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${REVISION} -X main.date=${BUILDTIME}" -o /out/bin/seasonpackarr cmd/seasonpackarr/main.go

# build runner
FROM alpine:latest as RUNNER

LABEL org.opencontainers.image.source = "https://github.com/nuxencs/seasonpackarr"
LABEL org.opencontainers.image.licenses = "GPL-2.0-or-later"
LABEL org.opencontainers.image.base.name = "alpine:latest"

ENV HOME="/config" \
    XDG_CONFIG_HOME="/config" \
    XDG_DATA_HOME="/config"

RUN apk add --no-cache ca-certificates curl tzdata jq

WORKDIR /app
VOLUME /config
EXPOSE 42069

COPY --link --from=app-builder /out/bin/seasonpackarr /usr/bin/


ENTRYPOINT ["/usr/bin/seasonpackarr", "start", "--config", "/config"]
