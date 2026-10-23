#!/bin/sh
set -eu

go test ./...
go build -o /tmp/creator-sweeper ./cmd/creator-sweeper
