#!/bin/sh

docker build . -t ghcr.io/gitdeath/device-mapping-manager:master
docker push ghcr.io/gitdeath/device-mapping-manager:master