# The trial of #132, reverted once docs/decisions.md records it: the exec agent, and the runner image with dulwich.
# Built with: docker build --target agent|runner --build-arg RUNNER_IMAGE=... scripts/kind/workspace-trial
ARG RUNNER_IMAGE=rotor-runner:dev
FROM golang:1.26.8-alpine@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c AS build
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-buildvcs=false
WORKDIR /src
COPY agent/ ./
RUN go build -trimpath -ldflags=-s -o /agent .

FROM scratch AS agent
COPY --from=build /agent /agent
ENTRYPOINT ["/agent"]

FROM python:3.13-slim@sha256:57b29a7c050c4e679e4bea572e14b3965cf4b5839de2da49d55e49ffc4024f39 AS deps
RUN pip install -q --no-compile --target /deps dulwich==1.2.17

FROM ${RUNNER_IMAGE} AS runner
COPY --from=deps /deps /opt/trial
COPY driver.py /opt/trial/driver.py
ENV PYTHONPATH=/opt/trial
