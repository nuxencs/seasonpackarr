# build base
FROM --platform=$BUILDPLATFORM golang:1.22-alpine3.19 AS app-base

WORKDIR /src

ENV SERVICE=seasonpackarr
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
    go build -ldflags "-s -w -X seasonpackarr/internal/buildinfo.Version=${VERSION} -X seasonpackarr/internal/buildinfo.Commit=${REVISION} -X seasonpackarr/internal/buildinfo.Date=${BUILDTIME}" -o /out/bin/seasonpackarr main.go

# build runner
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.source = "https://github.com/nuxencs/seasonpackarr" \
      org.opencontainers.image.licenses = "GPL-2.0-or-later" \
      org.opencontainers.image.base.name = "distroless/static-debian12:nonroot"

ENV HOME="/config" \
    XDG_CONFIG_HOME="/config" \
    XDG_DATA_HOME="/config"

WORKDIR /app
VOLUME /config
EXPOSE 42069

COPY --link --from=seasonpackarr /out/bin/seasonpackarr /usr/bin/

ENTRYPOINT ["/usr/bin/seasonpackarr", "start", "--config", "/config"]