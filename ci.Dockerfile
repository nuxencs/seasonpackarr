# build base
FROM --platform=$BUILDPLATFORM golang:1.22-alpine3.19 AS app-base

ENV SERVICE=seasonpackarr
WORKDIR /src
ARG VERSION=dev \
    REVISION=dev \
    BUILDTIME \
    TARGETOS TARGETARCH TARGETVARIANT

COPY go.mod go.sum ./
RUN go mod download
COPY . ./

# build seasonpackarr
FROM --platform=$BUILDPLATFORM app-base AS seasonpackarr
RUN --network=none --mount=target=. \
export GOOS=$TARGETOS; \
export GOARCH=$TARGETARCH; \
[[ "$GOARCH" == "amd64" ]] && export GOAMD64=$TARGETVARIANT; \
[[ "$GOARCH" == "arm" ]] && [[ "$TARGETVARIANT" == "v6" ]] && export GOARM=6; \
[[ "$GOARCH" == "arm" ]] && [[ "$TARGETVARIANT" == "v7" ]] && export GOARM=7; \
echo $GOARCH $GOOS $GOARM$GOAMD64; \
go build -ldflags "-s -w -X github.com/nuxencs/seasonpackarr/internal/buildinfo.Version=${VERSION} -X github.com/nuxencs/seasonpackarr/internal/buildinfo.Commit=${REVISION} -X github.com/nuxencs/seasonpackarr/internal/buildinfo.Date=${BUILDTIME}" -o /out/bin/seasonpackarr main.go

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