#!/bin/sh
set -eu
version='__GIT_TASK_VERSION__'
from_directory=''
while [ "$#" -gt 0 ]; do
    case "$1" in
        --version) [ "$#" -ge 2 ] || exit 2; version=$2; shift 2 ;;
        --from-directory) [ "$#" -ge 2 ] || exit 2; from_directory=$2; shift 2 ;;
        *) echo 'Usage: sh install.sh [--version VERSION] [--from-directory DIRECTORY]' >&2; exit 2 ;;
    esac
done
version=${version#v}
printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$' || { echo 'Specify --version, for example 0.1.0' >&2; exit 1; }
case $(uname -s) in Darwin) os=darwin ;; Linux) os=linux ;; *) echo 'Use install.ps1 on Windows' >&2; exit 1 ;; esac
case $(uname -m) in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) echo 'Unsupported CPU architecture' >&2; exit 1 ;; esac
case "$os/$arch" in darwin/amd64|darwin/arm64|linux/amd64) ;; *) echo 'No release archive for this platform' >&2; exit 1 ;; esac
git_version=$(git --version | awk '{print $3}')
printf '%s\n' "$git_version" | awk -F. '{if ($1 < 2 || ($1 == 2 && $2 < 51)) exit 1}' || { echo 'Git 2.51.0 or newer is required' >&2; exit 1; }
root=$(git rev-parse --show-toplevel)
managed_root="$root/.tools/git-task"
destination="$managed_root/$version/$os-$arch"
binary="$destination/git-task"
quote_path() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"; }
alias="!$(quote_path "$binary")"
old_alias=$(git config --local --get alias.task || test "$?" -eq 1)
if [ -n "$old_alias" ]; then
    old_binary=$(git config --local --get git-task.install-path || test "$?" -eq 1)
    case "$old_binary" in "$managed_root/"*) ;; *) echo 'Existing task alias belongs to another installation' >&2; exit 1 ;; esac
    [ "$old_alias" = "!$(quote_path "$old_binary")" ] || { echo 'Existing task alias is not managed by this installer' >&2; exit 1; }
    echo 'Uninstall hooks with the old binary and remove its local task alias/install-path before switching versions; see INSTALL.md' >&2
    exit 1
fi
[ ! -e "$destination" ] && [ ! -L "$destination" ] || { echo 'This version already exists; inspect the installed directory' >&2; exit 1; }
path=$destination
while [ "$path" != "$root" ]; do
    [ ! -L "$path" ] || { echo 'Symlink installation path refused' >&2; exit 1; }
    if [ -e "$path" ]; then [ -d "$path" ] || { echo 'Installation path is not a directory' >&2; exit 1; }; fi
    path=$(dirname "$path")
done
[ -z "$(git ls-files -- .tools/git-task)" ] || { echo 'Installation directory contains tracked files' >&2; exit 1; }
exclude=$(git rev-parse --path-format=absolute --git-path info/exclude)
exclude_parent=$(dirname "$exclude")
[ ! -L "$exclude_parent" ] || { echo 'Symlink info directory refused' >&2; exit 1; }
if [ -e "$exclude_parent" ]; then [ -d "$exclude_parent" ] || { echo 'info path is not a directory' >&2; exit 1; }; fi
[ ! -L "$exclude" ] || { echo 'Symlink info/exclude refused' >&2; exit 1; }
if [ -e "$exclude" ]; then [ -f "$exclude" ] || { echo 'info/exclude is not a regular file' >&2; exit 1; }; fi
mkdir -p "$root/.tools"
temporary=$(mktemp -d "$root/.tools/git-task-download-XXXXXXXX")
trap 'rm -rf -- "$temporary"' EXIT HUP INT TERM
archive_name="git-task_${version}_${os}_${arch}.tar.gz"
for name in SHA256SUMS "$archive_name"; do
    if [ -n "$from_directory" ]; then
        cp "$from_directory/$name" "$temporary/$name"
    else
        curl --fail --location --silent --show-error "https://github.com/ToothLonely/TaskPlugin/releases/download/v$version/$name" --output "$temporary/$name"
    fi
done
expected=$(awk -v name="$archive_name" '$2 == name && NF == 2 && length($1) == 64 && $1 ~ /^[0-9a-fA-F]+$/ {hash=$1; count++} END {if (count != 1) exit 1; print hash}' "$temporary/SHA256SUMS")
if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$temporary/$archive_name" | awk '{print $1}')
else
    actual=$(shasum -a 256 "$temporary/$archive_name" | awk '{print $1}')
fi
[ "$actual" = "$expected" ] || { echo 'Archive checksum mismatch' >&2; exit 1; }
tar -xOzf "$temporary/$archive_name" git-task > "$temporary/git-task"
chmod 755 "$temporary/git-task"
[ "$("$temporary/git-task" version)" = "git-task $version" ] || { echo 'Binary version differs from requested version' >&2; exit 1; }
mkdir -p "$destination"
cp "$temporary/git-task" "$binary"
chmod 755 "$binary"
mkdir -p "$exclude_parent"
if [ ! -f "$exclude" ] || ! grep -Fxq '/.tools/git-task/' "$exclude"; then printf '\n/.tools/git-task/\n' >> "$exclude"; fi
git config --local git-task.install-path "$binary"
git config --local alias.task "$alias"
printf 'Installed git-task %s for %s\n' "$version" "$root"
printf '%s\n' 'Next: git task init --target main; git task hooks install'
