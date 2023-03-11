#!/bin/bash
set -e
version=`git tag --sort=committerdate | tail -1`
inspect=`docker images -q dosco/graphjin:$version 2> /dev/null`

# if [[ "$inspect" == "" ]]; then
#   docker build --rm -t dosco/graphjin:$version -t dosco/graphjin:latest .
# fi

# docker login  
env KO_DOCKER_REPO=dosco/graphjin ko build --bare --tags=$version,latest --platform=linux/amd64,linux/arm64 ./cmd
