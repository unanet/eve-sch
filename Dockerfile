FROM unanet-docker.jfrog.io/alpine-base

ENV EVE_METRICS_PORT 3001
ENV EVE_SERVICE_NAME eve-sch

ADD ./bin/eve-sch /app/eve-sch
WORKDIR /app
CMD ["/app/eve-sch"]

HEALTHCHECK --interval=1m --timeout=2s --start-period=60s \
    CMD curl -f http://localhost:${EVE_METRICS_PORT}/ || exit 1
