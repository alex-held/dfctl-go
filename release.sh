#!/bin/bash
set -e

tag="${1}"

if [ "${tag}" == "" ]; then
  echo "tag argument required"
  exit 1
fi

rm -rf dist
GOOS=darwin GOARCH=amd64 go build -o "dist/darwin-amd64"
GOOS=darwin GOARCH=arm64 go build -o "dist/darwin-arm64"
GOOS=linux GOARCH=386 go build -o "dist/linux-i386"
GOOS=linux GOARCH=amd64 go build -o "dist/linux-amd64"

gh release create $tag ./dist/* --title="${tag}" --notes "${tag}"
