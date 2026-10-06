#!/usr/bin/env bash
set -euo pipefail
stage=${1:-P15}
case "$stage" in P15|P18) ;; *) echo 'Expected P15 or P18' >&2; exit 2 ;; esac
mkdir -p /work/project /work/tmp /work/cache /work/home
cp -R /source/go.mod /source/cmd /source/internal /source/scripts /work/project/
cd /work/project
export GOCACHE=/work/cache GOMODCACHE=/work/modcache GOPATH=/work/gopath
export GOTMPDIR=/work/tmp TMPDIR=/work/tmp
export GOENV=off GOTOOLCHAIN=local GOTELEMETRY=off CGO_ENABLED=1 GOPROXY=off GOWORK=off GOFLAGS=
export XDG_CONFIG_HOME=/work/home
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
export GIT_TERMINAL_PROMPT=0 GIT_ALLOW_PROTOCOL=file GIT_ASKPASS=git-task-no-askpass
export GIT_AUTHOR_NAME='CI Test' GIT_AUTHOR_EMAIL=ci@example.invalid
export GIT_COMMITTER_NAME='CI Test' GIT_COMMITTER_EMAIL=ci@example.invalid
mkdir -p /work/empty-template
export GIT_TEMPLATE_DIR=/work/empty-template
go version
git --version
gcc --version
go vet ./...
if [[ "$stage" == P18 ]]; then
    go test -count=1 -timeout=60m ./...
else
    python3 -c 'import json; g=json.load(open("scripts/p15-tests.json")); [print(x["package"]+"\t^("+"|".join(x["tests"])+")$") for x in g]' > /work/selected.tsv
    while IFS=$'\t' read -r package pattern; do
        go test -count=1 -timeout=15m -v -run "$pattern" "$package"
    done < /work/selected.tsv
fi
go build ./...
python3 -c 'import json; g=json.load(open("scripts/p15-tests.json")); [print(x["package"]+"\t^("+"|".join(x["race"])+")$") for x in g if x["race"]]' > /work/race.tsv
while IFS=$'\t' read -r package pattern; do
    go test -race -count=1 -timeout=15m -v -run "$pattern" "$package"
done < /work/race.tsv
