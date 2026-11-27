#!/usr/bin/env bash
# `make aur-check` (spec aur-packaging, "Package validation"): builds a
# goreleaser snapshot, then checks the generated omaslack-bin PKGBUILD in a
# clean archlinux:base-devel container. Local only: CI never runs it.
#
# The snapshot tarballs are put next to the PKGBUILD under the file names
# its source entries give, so makepkg uses them instead of downloading,
# and still verifies them against sha256sums_<arch>. The PKGBUILD itself is
# not modified.
set -euo pipefail

# archlinux:base-devel as of 2026-11-23. Override to try a newer one.
IMAGE=${AUR_CHECK_IMAGE:-docker.io/library/archlinux:base-devel@sha256:949ecb1661609ab7e8f6854ba03efd62792e7a796313738e94b040c095c20d91}

root=$(git rev-parse --show-toplevel)
cd "$root"

echo "==> goreleaser snapshot"
goreleaser release --snapshot --clean --skip=sign >/dev/null

work=$(mktemp -d "${TMPDIR:-/tmp}/omaslack-aur-check.XXXXXX")
trap 'rm -rf "$work"' EXIT
cp dist/aur/omaslack-bin.pkgbuild "$work/PKGBUILD"
cp dist/aur/omaslack-bin.srcinfo "$work/.SRCINFO"
cp packaging/aur/expected-files.txt packaging/aur/check-in-container.sh "$work/"

echo "==> sources and checksums"
if grep -q "SKIP" "$work/PKGBUILD"; then
  echo "FAIL: the PKGBUILD skips a checksum" >&2
  exit 1
fi
for arch in x86_64 aarch64; do
  entry=$(sed -n "s/^\tsource_${arch} = //p" "$work/.SRCINFO")
  want=$(sed -n "s/^\tsha256sums_${arch} = //p" "$work/.SRCINFO")
  name=${entry%%::*}
  url=${entry#*::}
  # The URL must end with the name of an archive goreleaser built.
  cp "dist/${url##*/}" "$work/$name"
  got=$(sha256sum "$work/$name" | cut -d' ' -f1)
  if [[ -z $want || $got != "$want" ]]; then
    echo "FAIL: $arch: sha256 $got, PKGBUILD says '$want'" >&2
    exit 1
  fi
  echo "$arch: $url"
  echo "$arch: sha256 $got matches"
done

echo "==> clean build in $IMAGE"
podman run --rm -v "$work:/work:ro,Z" "$IMAGE" bash /work/check-in-container.sh
echo "==> aur-check passed"
