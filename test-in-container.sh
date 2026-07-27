#!/usr/bin/env sh

set -e

docker run --rm -t --env="HOME=/tmp/" --user=nobody --volume $PWD:/src:ro --workdir=/src golang:1.26.3 \
    go test -v ./...
