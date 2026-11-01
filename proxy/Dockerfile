FROM docker.io/golang:1.24-alpine AS builder
WORKDIR /src
COPY proxy/healthcheck.go ./healthcheck.go
COPY proxy/entrypoint/main.go ./entrypoint/main.go
RUN CGO_ENABLED=0 go build -trimpath -o /proxy-healthcheck ./healthcheck.go \
    && CGO_ENABLED=0 go build -trimpath -o /proxy-entrypoint ./entrypoint/main.go \
    && mkdir /tls

FROM docker.io/tesla/vehicle-command@sha256:7cf13d00fe21c1967ffd12b87a9e66e645d4578ea0313b8b43bde07b82523d66
COPY --from=builder /proxy-healthcheck /usr/local/bin/proxy-healthcheck
COPY --from=builder /proxy-entrypoint /usr/local/bin/proxy-entrypoint
COPY --from=builder --chown=65532:65532 /tls /tls
ENTRYPOINT ["/usr/local/bin/proxy-entrypoint"]