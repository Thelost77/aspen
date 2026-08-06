#!/usr/bin/env bash

set -euo pipefail

version="${1:-}"
notes_file="docs/releases/${version}.md"

if [[ -z "$version" ]]; then
	echo "usage: $0 vX.Y.Z" >&2
	exit 1
fi

if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "version must match vX.Y.Z" >&2
	exit 1
fi

if [[ ! -f "$notes_file" ]]; then
	echo "missing release notes: $notes_file" >&2
	exit 1
fi

if [[ -n "$(git status --porcelain)" ]]; then
	echo "worktree is not clean" >&2
	exit 1
fi

if [[ "$(git branch --show-current)" != "main" ]]; then
	echo "release must run from main" >&2
	exit 1
fi

if ! git remote get-url origin >/dev/null 2>&1; then
	echo "origin remote is required" >&2
	exit 1
fi

if ! command -v gh >/dev/null 2>&1; then
	echo "gh is required to publish a GitHub release" >&2
	exit 1
fi

if ! gh auth status >/dev/null 2>&1; then
	echo "gh is not authenticated" >&2
	exit 1
fi

if git rev-parse "$version" >/dev/null 2>&1; then
	echo "tag $version already exists" >&2
	exit 1
fi

if git ls-remote --exit-code --tags origin "refs/tags/${version}" >/dev/null 2>&1; then
	echo "tag $version already exists on origin" >&2
	exit 1
fi

git tag -a "$version" -m "Aspen ${version}"
git push origin HEAD --follow-tags
gh release create "$version" --title "Aspen ${version}" --notes-file "$notes_file"
