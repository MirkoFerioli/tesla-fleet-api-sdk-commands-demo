FROM docker.io/golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY telemetry/runtime ./telemetry/runtime
RUN CGO_ENABLED=0 go build -trimpath -o /telemetry-runtime ./telemetry/runtime

FROM docker.io/tailscale/tailscale:v1.104.1 AS worker
COPY --from=builder /telemetry-runtime /telemetry-runtime
ENTRYPOINT ["/telemetry-runtime"]
CMD ["renew"]

FROM docker.io/tesla/fleet-telemetry:v0.9.5 AS receiver
COPY --from=builder /telemetry-runtime /telemetry-runtime
ENTRYPOINT ["/telemetry-runtime"]
CMD ["receive"]