CI_COMMIT_BRANCH ?= local
CI_COMMIT_SHORT_SHA ?= 000001
CI_PROJECT_ID ?= 0
CI_PIPELINE_IID ?= 0
GOPATH ?= ${HOME}/go
MODCACHE ?= ${GOPATH}/pkg/mod
BUILD_NUMBER := ${CI_PIPELINE_IID}
PATCH_VERSION := $(shell cat VERSION)
VERSION := ${PATCH_VERSION}.${BUILD_NUMBER}
SHA := $(shell git rev-parse HEAD)
SHORT_SHA := $(shell git rev-parse --short HEAD)
AUTHOR := $(shell git log --pretty='%cn' -n1 HEAD)
DOCKER_UID = $(shell id -u)
DOCKER_GID = $(shell id -g)
CUR_DIR := $(shell pwd)
BUILD_HOST := $(shell hostname)
BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
BUILD_DATE := $(shell /bin/date -u "+%Y%m%d%H%M%S")
BUILD_IMAGE := golang:alpine
IMAGE_NAME := eve-sch
PRERELEASE ?= 

docker-helm-exec = docker run --rm --user ${DOCKER_UID}:${DOCKER_UID} \
	-v ${CUR_DIR}:/src \
	-w /src \
	alpine/helm	

.PHONY: test build 

build:
	docker pull ${BUILD_IMAGE}
	docker build --build-arg VERSION=${VERSION} --build-arg SHA=${SHA} --build-arg SHORT_SHA=${SHORT_SHA} --build-arg AUTHOR='${AUTHOR}' --build-arg BUILD_HOST='${BUILD_HOST}' --build-arg BRANCH=${BRANCH} --build-arg BUILD_DATE='${BUILD_DATE}' --build-arg PRERELEASE='${PRERELEASE}' . -t ${IMAGE_NAME} -t ${IMAGE_NAME}:${PATCH_VERSION}

helm:
	$(docker-helm-exec) package --version ${PATCH_VERSION} --app-version ${VERSION} ./.helm

test:
	docker pull ${BUILD_IMAGE}
	go test -tags !local ./...
