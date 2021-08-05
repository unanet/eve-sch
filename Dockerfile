##########################################
# STEP 1 build binary in Build Stage Image
##########################################
FROM golang:alpine AS builder

# Build ARGS
ARG VERSION=0.0.0
ARG SHA=""
ARG SHORT_SHA=""
ARG AUTHOR=""
ARG BUILD_HOST=""
ARG BRANCH=""
ARG BUILD_DATE=""
ARG PRERELEASE=""


ENV LDFLAGS "-X 'github.com/unanet/go/pkg/version.Version=${VERSION}' \
               -X 'github.com/unanet/go/pkg/version.SHA=${SHA}' \ 
               -X 'github.com/unanet/go/pkg/version.ShortSHA=${SHORT_SHA}' \
               -X 'github.com/unanet/go/pkg/version.Author=${AUTHOR}' \
               -X 'github.com/unanet/go/pkg/version.BuildHost=${BUILD_HOST}' \
               -X 'github.com/unanet/go/pkg/version.Branch=${BRANCH}' \
               -X 'github.com/unanet/go/pkg/version.Date=${BUILD_DATE}' \
               -X 'github.com/unanet/go/pkg/version.Prerelease=${PRERELEASE}'"

# Install git + SSL ca certificates.
# Git is required for fetching the dependencies.
# Ca-certificates is required to call HTTPS endpoints.
# tzdata is for timezone data
RUN apk update && apk add --no-cache git ca-certificates tzdata && update-ca-certificates

# create appuser.
RUN adduser -D -g '' appuser

# set app working dir
WORKDIR /src

COPY go.mod go.sum ./

RUN \
    go mod tidy && \
    go mod download

COPY . .

RUN \
    CGO_ENABLED=0 \
    GOOS=linux GOARCH=amd64 \
    go build -ldflags="${LDFLAGS}" -o /bin/eve-sch ./cmd/eve-sch/main.go

######################################
# STEP 2 build a smaller runtime image
######################################
# FROM scratch
FROM plainsight.jfrog.io/docker/alpine:3.1

# Import assets from the build stage image
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /bin/eve-sch /bin/eve-sch

WORKDIR /bin

# Use the unprivileged user (created in the build stage image)
USER appuser

# Set the entrypoint to the golang executable binary
CMD ["/bin/eve-sch"]

