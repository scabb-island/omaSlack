#!/bin/sh
# The release job's script (.gitlab-ci.yml, `release`), run in the
# goreleaser image on vX.Y.Z tags. Arguments go to `goreleaser release`;
# a local dry run in that image passes `--snapshot --skip=sign`.
#
# Order matters: tests, build, archives, checksums and the cosign
# signature all happen before goreleaser publishes anything, and the AUR
# push is its last publisher, after the GitLab release and its assets. A
# failure at any step stops the job before the AUR is touched.
set -eu

log() { printf 'release: %s\n' "$*"; }
fail() { printf 'release: %s\n' "$*" >&2; exit 1; }

dry_run=false
case " $* " in
  *" --snapshot "*) dry_run=true ;;
esac

# cosign, pinned by version and checksum (COSIGN_VERSION, COSIGN_SHA256 in
# .gitlab-ci.yml). It goes first on PATH, ahead of the image's own copy.
if [ -n "${COSIGN_VERSION:-}" ]; then
  wget -qO /tmp/cosign "https://github.com/sigstore/cosign/releases/download/v${COSIGN_VERSION}/cosign-linux-amd64"
  echo "${COSIGN_SHA256:?}  /tmp/cosign" | sha256sum -c - >/dev/null || fail "cosign ${COSIGN_VERSION}: checksum mismatch"
  install -m 0755 /tmp/cosign /usr/local/bin/cosign
  log "cosign ${COSIGN_VERSION} installed"
fi

# Keyless signing needs the job's OIDC token (id_tokens in .gitlab-ci.yml).
if [ "$dry_run" = false ] && [ -z "${SIGSTORE_ID_TOKEN:-}" ]; then
  fail "SIGSTORE_ID_TOKEN is not set: checksums.txt cannot be signed"
fi

# The AUR is pushed only from a push pipeline of a protected vX.Y.Z tag,
# and only when the AUR_SSH_KEY variable exists. It holds the private key
# base64-encoded on one line, because GitLab can only mask single-line
# values; the variable is protected, so only protected tags can read it.
# Anywhere else the key is dropped and goreleaser skips the upload
# (skip_upload in .goreleaser.yaml). The key is never printed.
aur_allowed() {
  printf '%s\n' "${CI_COMMIT_TAG:-}" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' &&
    [ "${CI_COMMIT_REF_PROTECTED:-}" = true ] &&
    [ "${CI_PIPELINE_SOURCE:-}" = push ] &&
    [ "$dry_run" = false ]
}
if [ -z "${AUR_SSH_KEY:-}" ]; then
  log "AUR_SSH_KEY is not set: omaslack-bin will not be pushed to the AUR"
elif ! aur_allowed; then
  log "not a push of a protected vX.Y.Z tag: omaslack-bin will not be pushed to the AUR"
  unset AUR_SSH_KEY
else
  [ -n "${AUR_MAINTAINER:-}" ] || fail "AUR_MAINTAINER (\"Name <email>\") is not set; the PKGBUILD needs it"
  key_file=$(mktemp)
  chmod 600 "$key_file"
  printf '%s' "$AUR_SSH_KEY" | base64 -d >"$key_file" 2>/dev/null ||
    fail "AUR_SSH_KEY is not valid base64"
  grep -q -- '-----BEGIN OPENSSH PRIVATE KEY-----' "$key_file" ||
    fail "AUR_SSH_KEY does not decode to an OpenSSH private key"
  # goreleaser takes a path in private_key.
  export AUR_SSH_KEY="$key_file"
  log "omaslack-bin will be pushed to the AUR after the GitLab release"
fi

log "go test"
go test ./...

exec goreleaser release --clean "$@"
