#!/usr/bin/env bash
set -euxo pipefail

# Work on a Linux filesystem, not a Windows bind mount. Only source inputs
# are mounted; the repository's Git data and host tool caches are not exposed.
mkdir -p /work/project /work/tmp /work/cache /work/home
cp -R /source/go.mod /source/cmd /source/internal /work/project/
cd /work/project
export HOME=/work/home
export GOCACHE=/work/cache GOMODCACHE=/work/modcache GOPATH=/work/gopath
export GOTMPDIR=/work/tmp TMPDIR=/work/tmp
export GOENV=off GOTOOLCHAIN=local GOTELEMETRY=off CGO_ENABLED=1

go version
git --version
gcc --version
go vet ./...
go test -count=1 ./...
go build ./...
go test -count=1 -v -run 'TestTracked(StorageCaseAliases|CaseDistinctDirectoriesRemainUsable)|TestSymlinkStoragePreservesDestination' ./internal/storage
go test -race -count=1 ./internal/storage ./internal/cli ./internal/git
