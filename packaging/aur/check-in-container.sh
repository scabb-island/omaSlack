#!/usr/bin/env bash
# The container half of `make aur-check` (packaging/aur/check.sh), run as
# root in a fresh archlinux:base-devel with the PKGBUILD, .SRCINFO and
# tarballs in /work (read-only).
set -euo pipefail

fail() { echo "FAIL: $*" >&2; exit 1; }

pacman -Syu --noconfirm --needed namcap >/dev/null 2>&1 || fail "pacman could not install namcap"

useradd -m builder
install -d -o builder /build
cp -a /work/. /build/
chown -R builder /build
cd /build
as_builder() { runuser -u builder -- "$@"; }

echo "--> .SRCINFO matches makepkg --printsrcinfo"
# goreleaser orders the per-arch keys differently; the content must match.
normalize() { sed 's/^[[:space:]]*//' "$1" | grep -v '^$' | sort; }
as_builder makepkg --printsrcinfo >/tmp/srcinfo.makepkg
diff <(normalize .SRCINFO) <(normalize /tmp/srcinfo.makepkg) || fail ".SRCINFO differs from the PKGBUILD"

echo "--> namcap PKGBUILD"
namcap PKGBUILD | tee /tmp/namcap.pkgbuild
grep -q ' E: ' /tmp/namcap.pkgbuild && fail "namcap reports errors in the PKGBUILD"

echo "--> makepkg"
as_builder makepkg --noconfirm --nodeps 2>&1 | sed 's/^/    /'
# makepkg also writes omaslack-bin-debug when debug is in OPTIONS (the
# default); only the main package is checked and installed.
pkg=$(ls omaslack-bin-[0-9]*.pkg.tar.zst)
echo "built $pkg"

echo "--> namcap $pkg"
namcap "$pkg" | tee /tmp/namcap.pkg
grep -q ' E: ' /tmp/namcap.pkg && fail "namcap reports errors in the package"

echo "--> file list"
pacman -Qlp "$pkg" | awk '{print $2}' | grep -v '/$' | sort >/tmp/files
diff expected-files.txt /tmp/files || fail "the package does not install exactly the spec's files"
cat /tmp/files
if bsdtar -tf "$pkg" | grep -qx '.INSTALL'; then
  fail "the package has an install script"
fi

echo "--> install, writes nothing under /home or /root"
home_state() { find /home /root -xdev -printf '%p %s %T@\n' | grep -v '^/home/builder' | sort; }
home_state >/tmp/home.before
pacman -U --noconfirm "$pkg" >/dev/null
home_state >/tmp/home.after
diff /tmp/home.before /tmp/home.after || fail "installing changed the home directories"
pacman -Qi omaslack-bin | grep -E '^(Name|Version|Provides|Conflicts With|Optional Deps)|^ {10,}'
omaslack version

echo "--> remove, leaves nothing behind"
pacman -Rns --noconfirm omaslack-bin >/dev/null
while read -r f; do
  [[ ! -e $f ]] || fail "$f is still there after removal"
done <expected-files.txt
[[ ! -e /usr/share/omaslack ]] || fail "/usr/share/omaslack is still there after removal"
echo "all removed"
