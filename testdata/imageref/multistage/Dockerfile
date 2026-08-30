# syntax=docker/dockerfile:1
ARG GO_VERSION=1.26

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
WORKDIR /src
RUN go build -o /out/app ./cmd/app

FROM golang:${GO_VERSION}-alpine AS argbased
RUN true

FROM builder AS second
RUN true

FROM alpine:3.22
COPY --from=builder /out/app /usr/local/bin/app
COPY --from=busybox:1.37 /bin/busybox /bin/busybox
COPY --from=0 /out/app /app
