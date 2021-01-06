FROM unanet-docker.jfrog.io/alpine-base

ENV EVE_PORT 3000
ENV EVE_METRICS_PORT 3001
ENV VAULT_ADDR https://vault.unanet.io
ENV VAULT_ROLE eve-sch

ADD ./bin/eve-sch /app/eve-sch
WORKDIR /app
CMD ["/app/eve-sch"]

HEALTHCHECK --interval=1m --timeout=2s --start-period=60s \
    CMD curl -f ht  tp://localhost:${EVE_METRICS_PORT}/ || exit 1
