#!/bin/sh

if ! which dep >/dev/null 2>&1; then
  go get -u github.com/golang/dep/cmd/dep
fi
if ! which golint >/dev/null 2>&1; then
  go get -u golang.org/x/lint/golint
fi
if ! which gosec >/dev/null 2>&1; then
  go get github.com/securego/gosec/cmd/gosec/...
fi
