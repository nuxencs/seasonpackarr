# build app
FROM golang:1.20-alpine3.19 AS app-builder

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

RUN go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${REVISION} -X main.date=${BUILDTIME}" -o bin/seasonpackarr cmd/seasonpackarr/main.go

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
