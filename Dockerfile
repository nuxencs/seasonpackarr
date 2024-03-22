# build app
FROM golang:1.22-alpine3.19 AS app-builder

ARG VERSION=dev
ARG REVISION=dev
ARG BUILDTIME

RUN apk add --no-cache git build-base tzdata

ENV SERVICE=seasonpackarr

WORKDIR /src

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

COPY . ./

#ENV GOOS=linux
ENV CGO_ENABLED=0

RUN go build -ldflags "-s -w -X github.com/nuxencs/seasonpackarr/internal/buildinfo.Version=${VERSION} -X github.com/nuxencs/seasonpackarr/internal/buildinfo.Commit=${REVISION} -X github.com/nuxencs/seasonpackarr/internal/buildinfo.Date=${BUILDTIME}" -o bin/seasonpackarr main.go

# build runner
FROM alpine:latest

LABEL org.opencontainers.image.source = "https://github.com/nuxencs/seasonpackarr"

ENV HOME="/config" \
    XDG_CONFIG_HOME="/config" \
    XDG_DATA_HOME="/config"

RUN apk add --no-cache ca-certificates curl tzdata jq

WORKDIR /app
VOLUME /config
EXPOSE 42069

COPY --from=app-builder /src/bin/seasonpackarr /usr/bin/


ENTRYPOINT ["/usr/bin/seasonpackarr", "start", "--config", "/config"]
