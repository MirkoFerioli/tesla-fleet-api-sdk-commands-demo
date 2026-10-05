FROM golang:1.24-alpine AS healthcheck-builder
WORKDIR /src
COPY proxy/healthcheck.go .
RUN CGO_ENABLED=0 go build -o /proxy-healthcheck healthcheck.go

FROM docker.io/tesla/vehicle-command:latest
COPY --from=healthcheck-builder /proxy-healthcheck /usr/local/bin/proxy-healthcheck