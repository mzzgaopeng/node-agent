#!/bin/bash

rm -rf ./node-agent

#当前版本号,每次更新服务时都必须更新版本号
CurrentVersion=1.0.0
Path=node-agent

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-X $Path.Version=$CurrentVersion" -x -o node-agent  node-agent.go
#GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -v -i -o node-agent  node-agent.go
