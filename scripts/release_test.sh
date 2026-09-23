#!/usr/bin/env bash
# Runs the release flow end to end in a scratch repository: prepare, the
# refusals, publish, and the tag signature check. Tags are annotated rather
# than signed, because a signed tag needs the release key.
set -euo pipefail

src=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "ok   $*"; }

# must_refuse <what> <cmd...>: the command must fail.
must_refuse() {
	local what=$1
	shift
	if "$@" >"$work/out" 2>&1; then
		cat "$work/out" >&2
		fail "$what was accepted"
	fi
	pass "refuses $what"
}

export GIT_AUTHOR_NAME=test GIT_AUTHOR_EMAIL=test@example.com
export GIT_COMMITTER_NAME=test GIT_COMMITTER_EMAIL=test@example.com
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export LV_GIT_TAG_ARGS=-a LV_RELEASE_CHECK=true

repo=$work/repo
mkdir -p "$repo/scripts" "$repo/changelog"
cp "$src/scripts/release.sh" "$src/scripts/verify-release-tag.sh" "$repo/scripts/"
cp "$src/changelog/TEMPLATE.md" "$repo/changelog/"
printf '{\n  outputs = { self }: let\n      version = "0.1.0";\n    in { };\n}\n' >"$repo/flake.nix"
cd "$repo"
git init -q -b master
git add -A
git commit -q -m "Start"
echo a >a && git add a && git commit -q -m "Add the first feature"

release() { scripts/release.sh "$@"; }
unmark() { grep -v 'lv-release:' "$1" >"$1.tmp" && mv "$1.tmp" "$1"; }

# The first release takes the flake's version as it stands.
release prepare >/dev/null
[ -f changelog/v0.1.0.md ] || fail "prepare wrote no changelog/v0.1.0.md"
grep -q '^# lv v0.1.0$' changelog/v0.1.0.md || fail "changelog has no title"
grep -q 'Add the first feature' changelog/v0.1.0.md || fail "changelog lists no commits"
grep -q 'version = "0.1.0";' flake.nix || fail "first release moved the flake version"
pass "prepare drafts the first release at the flake's version"

must_refuse "a second prepare over a draft" release prepare
must_refuse "a changelog with lv-release: lines left in" release publish

unmark changelog/v0.1.0.md
echo stray >stray
must_refuse "publishing with unrelated changes" release publish
rm stray

LV_RELEASE_CHECK=false must_refuse "publishing when the check fails" release publish

release publish >/dev/null
[ "$(git log -1 --format=%s)" = "Release v0.1.0" ] || fail "no release commit"
[ -z "$(git status --porcelain)" ] || fail "publish left the tree dirty"
[ "$(git rev-parse 'v0.1.0^{commit}')" = "$(git rev-parse HEAD)" ] || fail "tag is not on the release commit"
[ "$(git cat-file -t v0.1.0)" = tag ] || fail "tag is not annotated"
git cat-file -p v0.1.0 | grep -q '^# lv v0.1.0$' || fail "tag message lost the changelog title"
pass "publish commits and tags the release"

must_refuse "publishing a version that is already tagged" release publish

echo b >b && git add b && git commit -q -m "Add the second feature"
echo c >c && git add c && git commit -q -m "Match '\\b\\t' literally"
release prepare >/dev/null
grep -q 'version = "0.1.1";' flake.nix || fail "patch bump did not reach the flake"
grep -q 'Add the second feature' changelog/v0.1.1.md || fail "changelog lists no new commits"
grep -qF "Match '\\b\\t' literally" changelog/v0.1.1.md || fail "a backslash in a subject was mangled"
grep -q 'Add the first feature' changelog/v0.1.1.md && fail "changelog lists commits of the last release"
pass "prepare bumps the patch version and lists commits since the last tag"
git checkout -q -- flake.nix && rm changelog/v0.1.1.md

release prepare --minor >/dev/null
grep -q 'version = "0.2.0";' flake.nix || fail "--minor did not give 0.2.0"
git checkout -q -- flake.nix && rm changelog/v0.2.0.md
release prepare --major >/dev/null
grep -q 'version = "1.0.0";' flake.nix || fail "--major did not give 1.0.0"
git checkout -q -- flake.nix && rm changelog/v1.0.0.md
pass "prepare bumps minor and major"

echo dirty >>a
must_refuse "preparing on a dirty tree" release prepare
git checkout -q -- a

# The signature gate, against a throwaway key standing in for the release key.
if ! command -v gpg >/dev/null; then
	echo "skip tag signature checks: no gpg"
	exit 0
fi
export GNUPGHOME=$work/gnupg
mkdir -m 700 "$GNUPGHOME"
gpg --batch --quiet --passphrase '' --quick-gen-key 'lv test <test@example.com>' ed25519 sign never 2>/dev/null
fpr=$(gpg --batch --with-colons --list-secret-keys 2>/dev/null | awk -F: '/^fpr:/{print $10; exit}')

git tag -s -u "$fpr" -m signed v9.9.9 2>/dev/null
LV_RELEASE_KEY=$fpr scripts/verify-release-tag.sh v9.9.9 >/dev/null || fail "a tag signed by the release key was rejected"
pass "accepts a tag signed by the release key"
must_refuse "a tag signed by another key" scripts/verify-release-tag.sh v9.9.9
LV_RELEASE_KEY=$fpr must_refuse "an unsigned annotated tag" scripts/verify-release-tag.sh v0.1.0
git tag v9.9.8
LV_RELEASE_KEY=$fpr must_refuse "a lightweight tag" scripts/verify-release-tag.sh v9.9.8
