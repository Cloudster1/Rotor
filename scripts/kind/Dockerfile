# The tools the Kubernetes tests need, in one image, so nothing is installed on the host and a machine needs only
# Docker: kind to make a cluster out of a container, the Docker client it drives, kubectl and helm. Adapted from
# Exciter's scripts/kind/Dockerfile; every version is pinned, the node image in mk/kind.mk.
FROM golang:1.26-alpine AS kind
ENV GOFLAGS=-buildvcs=false GOTOOLCHAIN=local CGO_ENABLED=0
RUN go install sigs.k8s.io/kind@v0.30.0

FROM alpine/helm:3.19.0 AS helm
FROM alpine/kubectl:1.34.1 AS kubectl

FROM alpine:3.22
RUN apk add --no-cache bash curl docker-cli jq
COPY --from=kind /go/bin/kind /usr/local/bin/kind
COPY --from=helm /usr/bin/helm /usr/local/bin/helm
COPY --from=kubectl /usr/local/bin/kubectl /usr/local/bin/kubectl
