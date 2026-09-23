#!/usr/bin/env bash
# Succeed only if the tag carries a good signature certified by the release
# key. The release workflow and scripts/release.sh both run this, so the gate
# exists once.
#
#   scripts/verify-release-tag.sh <tag>
set -euo pipefail

tag=${1:?usage: verify-release-tag.sh <tag>}

# Primary key of the release identity. A signature from any valid subkey it
# certifies is accepted, so rotating a subkey needs no change here.
release_key=${LV_RELEASE_KEY:-D8861B9C6B9C4284D980D6016804DFEE281234B2}

# Importing the account's published keys is not a gate on its own: they may
# include keys other than the release key. gpg names the certifying primary in
# VALIDSIG's last field, and reports GOODSIG only for a key that is neither
# expired nor revoked.
status=$(git verify-tag --raw "$tag" 2>&1 || true)
primary=$(awk '/^\[GNUPG:\] VALIDSIG /{print $NF}' <<<"$status")
if ! grep -q '^\[GNUPG:\] GOODSIG ' <<<"$status" || [ "$primary" != "$release_key" ]; then
	printf '%s\n' "$status" >&2
	echo "::error::tag $tag is not signed by the release key $release_key" >&2
	exit 1
fi
echo "tag $tag is signed by the release key"
