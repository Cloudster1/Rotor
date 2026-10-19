# The tools the Kubernetes tests and the supply chain targets need, in one image, so nothing is installed on the host
# and a machine needs only Docker: kind to make a cluster out of a container, the Docker client it drives, kubectl and
# helm, and cosign and syft to sign images and list what is in them (#42). Adapted from Exciter's
# scripts/kind/Dockerfile; every version is pinned, the node image in mk/kind.mk.
FROM golang:1.26-alpine AS kind
ENV GOFLAGS=-buildvcs=false GOTOOLCHAIN=local CGO_ENABLED=0
RUN go install sigs.k8s.io/kind@v0.30.0

FROM alpine/helm:3.19.0 AS helm
FROM alpine/kubectl:1.34.1 AS kubectl
FROM ghcr.io/sigstore/cosign/cosign:v3.1.3@sha256:9e5c2f2edc34351160407ca3416c61855bdf9403c3c5936e0f0be7fc261611b8 AS cosign
FROM anchore/syft:v1.54.0@sha256:0356562f495d432056237fbea5cbc2d4839c9c75cd500784a66de2e7cc95ca7c AS syft

FROM alpine:3.22
RUN apk add --no-cache bash curl docker-cli jq
COPY --from=kind /go/bin/kind /usr/local/bin/kind
COPY --from=helm /usr/bin/helm /usr/local/bin/helm
COPY --from=kubectl /usr/local/bin/kubectl /usr/local/bin/kubectl
COPY --from=cosign /ko-app/cosign /usr/local/bin/cosign
COPY --from=syft /syft /usr/local/bin/syft
