#!/usr/bin/env bash
# Cut an lv release: one commit carrying the version bump and its changelog,
# and a signed tag on it. Nothing is pushed; pushing the tag to origin and to
# github is what starts the release, and that stays a person's decision.
#
#   scripts/release.sh prepare [--minor|--major]
#       edit changelog/vX.Y.Z.md, deleting every lv-release: line
#   scripts/release.sh publish
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# -s is the only value a release uses. The test overrides it, because a signed
# tag needs the release key.
read -r -a tag_args <<<"${LV_GIT_TAG_ARGS:--s}"
check_cmd=${LV_RELEASE_CHECK:-make check}

die() {
	echo "release: $*" >&2
	exit 1
}

flake_version() {
	sed -n 's/^ *version = "\([0-9]*\.[0-9]*\.[0-9]*\)";$/\1/p' flake.nix
}

last_tag() {
	git tag -l 'v[0-9]*.[0-9]*.[0-9]*' --sort=-version:refname | head -n1
}

# The first release is the flake's version as written; after that, the last
# tag bumped by patch unless told otherwise.
next_version() {
	local bump=$1 base major minor patch
	base=$(last_tag)
	if [ -z "$base" ]; then
		base=$(flake_version)
		[ -n "$base" ] || die "no version = \"X.Y.Z\"; line in flake.nix"
		[ -n "$bump" ] || {
			echo "$base"
			return
		}
	fi
	IFS=. read -r major minor patch <<<"${base#v}"
	case ${bump:-patch} in
	major) echo "$((major + 1)).0.0" ;;
	minor) echo "$major.$((minor + 1)).0" ;;
	patch) echo "$major.$minor.$((patch + 1))" ;;
	esac
}

require_clean_tree() {
	[ -z "$(git status --porcelain)" ] || die "the working tree has changes; commit or stash them first"
}

prepare() {
	local bump=
	case ${1:-} in
	"") ;;
	--minor) bump=minor ;;
	--major) bump=major ;;
	*) die "prepare takes --minor or --major, not $1" ;;
	esac
	require_clean_tree

	local version tag log last
	version=$(next_version "$bump")
	tag=v$version
	log=changelog/$tag.md
	last=$(last_tag)
	git rev-parse -q --verify "refs/tags/$tag" >/dev/null && die "$tag is already tagged"

	sed "s/^\( *version = \"\)[0-9.]*\(\";\)$/\1$version\2/" flake.nix >flake.nix.tmp
	mv flake.nix.tmp flake.nix

	local changes
	changes=$(git log --no-merges --format='- %s (%h)' ${last:+"$last.."}HEAD)
	# Through the environment, not awk -v, which would expand the escapes in a
	# subject such as "Match '\b' literally".
	VERSION=$tag LAST=${last:-the start} CHANGES=$changes awk '
		/^@CHANGES@$/ { print ENVIRON["CHANGES"]; next }
		{ gsub(/@VERSION@/, ENVIRON["VERSION"]); gsub(/@LAST@/, ENVIRON["LAST"]); print }
	' changelog/TEMPLATE.md >"$log"

	echo "Drafted $log for $tag."
	echo "Edit it, delete every lv-release: line, then run: scripts/release.sh publish"
}

publish() {
	local version tag log
	version=$(flake_version)
	tag=v$version
	log=changelog/$tag.md
	git rev-parse -q --verify "refs/tags/$tag" >/dev/null && die "$tag is already tagged; run prepare for the next version"
	[ -f "$log" ] || die "no $log; run prepare first"
	grep -q 'lv-release:' "$log" && die "$log still has lv-release: lines"

	local stray
	stray=$(git status --porcelain | awk '{print $2}' | grep -vx -e flake.nix -e "$log" || true)
	[ -z "$stray" ] || die "changes besides flake.nix and $log: $(echo "$stray" | tr '\n' ' ')"

	bash -c "$check_cmd" || die "'$check_cmd' failed"

	git add flake.nix "$log"
	git commit -q -s -m "Release $tag"
	# verbatim: the changelog's '# lv vX.Y.Z' title would be stripped as a
	# comment under git's default cleanup.
	git tag "${tag_args[@]}" --cleanup=verbatim -F "$log" "$tag"
	if [ "${tag_args[*]}" = -s ]; then
		scripts/verify-release-tag.sh "$tag" >/dev/null
	fi

	local branch
	branch=$(git symbolic-ref --short HEAD)
	echo "Tagged $tag. To release it:"
	echo "  git push origin $branch $tag"
	echo "  git push github $branch $tag"
}

case ${1:-} in
prepare) prepare "${@:2}" ;;
publish) publish ;;
*) die "usage: scripts/release.sh prepare [--minor|--major] | publish" ;;
esac
