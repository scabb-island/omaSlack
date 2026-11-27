## ADDED Requirements

### Requirement: AUR package identity
The binary SHALL be distributed as the AUR package `omaslack-bin`, built from the release artifacts and compliant with the Arch package guidelines and the AUR submission guidelines. Its PKGBUILD SHALL set `provides=('omaslack')` and `conflicts=('omaslack')`, so a future source package can replace it, and SHALL declare `arch=('x86_64' 'aarch64')`, `license=('MIT')`, `url` and `pkgdesc`.

#### Scenario: PKGBUILD metadata
- **WHEN** the generated PKGBUILD for a release is inspected
- **THEN** it has `pkgname=omaslack-bin`, `provides`, `conflicts`, both architectures, the MIT license, the project URL and a description

### Requirement: Package versioning
The PKGBUILD SHALL take `pkgver` from the release tag without the leading `v`, and SHALL reset `pkgrel` to 1 on every new version.

#### Scenario: Version from the tag
- **WHEN** the release for tag `v0.3.0` generates the PKGBUILD
- **THEN** it has `pkgver=0.3.0` and `pkgrel=1`

### Requirement: Verified sources
The PKGBUILD SHALL download one release tarball per architecture and verify each with a `sha256sums_<arch>` entry. It MUST NOT use `SKIP`.

#### Scenario: Checksums
- **WHEN** the PKGBUILD is inspected
- **THEN** `sha256sums_x86_64` and `sha256sums_aarch64` hold the real checksums of the release tarballs

### Requirement: Signed releases
Each release SHALL publish `checksums.txt` with a keyless cosign signature bundle (`checksums.txt.sigstore.json`) made in the release job with the GitLab CI OIDC identity, so no signing key is stored anywhere. The release notes SHALL show the `cosign verify-blob` command with the expected identity and issuer.

#### Scenario: Verifying a release
- **WHEN** a user downloads `checksums.txt` and its bundle from a release and runs the documented `cosign verify-blob` command
- **THEN** the signature verifies against the project's release job identity, and every tarball matches its line in `checksums.txt`

### Requirement: Optional dependencies
The package SHALL list in `optdepends`, not in `depends`, what improves omaslack but is not required: a Secret Service provider (`gnome-keyring` or `kwallet`) and a terminal with graphics support for inline images.

#### Scenario: Installing on a system without a keyring
- **WHEN** `omaslack-bin` is installed on a system with no Secret Service provider
- **THEN** the installation succeeds and pacman lists the optional dependencies

### Requirement: Installed file layout
The package SHALL install only these files: `/usr/bin/omaslack`, `/usr/lib/systemd/user/omaslack.service`, `/usr/share/applications/omaslack.desktop`, `/usr/share/omaslack/omaslack.toml.tpl`, `/usr/share/omaslack/theme-set.hook`, `/usr/share/omaslack/hypr/omaslack.lua` (the window rule `omaslack setup` offers), and the `LICENSE` and `THIRD_PARTY_NOTICES.md` files under `/usr/share/licenses/omaslack-bin/`.

#### Scenario: File list
- **WHEN** `pacman -Ql omaslack-bin` runs after installation
- **THEN** it lists only the files above

#### Scenario: Removal leaves no system files
- **WHEN** the user runs `pacman -Rns omaslack-bin`
- **THEN** no file installed by the package remains on the system

### Requirement: No writes to the home directory
The package MUST NOT write to `$HOME`. Per-user setup (copying the theme template and hook to `~/.config/omarchy/` and enabling the user service) SHALL be done by the `omaslack setup` command.

#### Scenario: Fresh install
- **WHEN** `omaslack-bin` is installed and the user has not run `omaslack setup`
- **THEN** nothing under the user's home directory has changed

### Requirement: Per-user setup
`omaslack setup` SHALL copy the theme template and the executable theme hook from `/usr/share/omaslack/` to `~/.config/omarchy/themed/` and `~/.config/omarchy/hooks/theme-set.d/`, render the theme once, and enable and start the user service. With `--window-rule` it SHALL also install the Hyprland window rule and say how to load it, without editing the user's Hyprland config. Running it twice SHALL change nothing the second time.

#### Scenario: Second run
- **WHEN** the user runs `omaslack setup` twice
- **THEN** the second run reports every item as already in place and changes nothing

### Requirement: Setup keeps the user's changes
`omaslack setup` SHALL NOT overwrite a file the user changed; it SHALL report that a newer version is available and how to compare. `omaslack setup --remove` SHALL disable the service, remove only the files `setup` installed that the user did not change, and say how to remove credentials and data.

#### Scenario: Edited template
- **WHEN** `~/.config/omarchy/themed/omaslack.toml.tpl` differs from the packaged one
- **THEN** `setup` leaves it untouched and reports that a newer version is available

#### Scenario: Removal
- **WHEN** the user runs `omaslack setup --remove`
- **THEN** the user service is disabled and the unchanged template, hook and window rule are gone, and the output says how to remove credentials (`omaslack auth remove`) and data

### Requirement: Package validation
Every PKGBUILD SHALL pass `namcap` without errors, ship a `.SRCINFO` generated from it with `makepkg --printsrcinfo`, and build and install in a clean Arch environment. The repository SHALL provide `make aur-check` to run these checks locally in an `archlinux:base-devel` container, outside CI.

#### Scenario: Local check before a release
- **WHEN** a maintainer runs `make aur-check`
- **THEN** `namcap` reports no errors, `.SRCINFO` matches the PKGBUILD and the package builds and installs in the container

### Requirement: Publish to the AUR only on release
The AUR package SHALL be published only by the release pipeline of a version tag (`vX.Y.Z`), after the GitLab release and its artifacts exist. Branch pipelines, merge request pipelines and manual runs SHALL NOT push to the AUR.

#### Scenario: Merge request pipeline
- **WHEN** a merge request pipeline runs
- **THEN** nothing is pushed to the AUR

#### Scenario: Tagged release
- **WHEN** the tag `v0.3.0` is pushed and the release job finishes building and uploading the artifacts
- **THEN** the AUR package `omaslack-bin` is updated to version 0.3.0 with the checksums of those artifacts

#### Scenario: Release job fails before upload
- **WHEN** the release job fails before the artifacts are uploaded
- **THEN** the AUR package is not updated

### Requirement: Protected AUR credentials
The AUR SSH key SHALL be stored in a protected, masked CI variable that only pipelines of protected tags can read.

#### Scenario: Branch pipeline
- **WHEN** a job runs in a branch or merge request pipeline
- **THEN** the AUR key variable is not available to it

### Requirement: The plugin checks for the package
The Omarchy plugin manifest has no field for package dependencies and `omarchy plugin add` runs no install hooks. `Service.qml` SHALL therefore check at runtime, with `omarchy-pkg-present`, that `omaslack-bin` (or another package that provides `omaslack`) is installed, and that `omaslack version` reports a compatible protocol. While either check fails, the plugin SHALL NOT try to start the daemon.

#### Scenario: Plugin installed without the package
- **WHEN** the user installs the plugin with `omarchy plugin add` and `omaslack-bin` is not installed
- **THEN** the bar icon shows the "not installed" state and no daemon start is attempted

### Requirement: The plugin offers to install the package
While the package is missing or incompatible, the panel SHALL say what is missing and offer an "Install omaslack-bin" or "Update omaslack-bin" action. The action SHALL run `omarchy-pkg-aur-add omaslack-bin` in a visible terminal (`omarchy-launch-floating-terminal-with-presentation`), so the user sees and authorizes it. The plugin MUST NOT install packages silently.

#### Scenario: Installing from the panel
- **WHEN** the user picks "Install omaslack-bin" in the panel
- **THEN** a floating terminal runs `omarchy-pkg-aur-add omaslack-bin`, the user confirms with their password, and once it finishes the plugin detects the package and continues with the normal flow

#### Scenario: Incompatible version
- **WHEN** the installed `omaslack` reports a protocol major version different from the one the plugin expects
- **THEN** the panel shows "Update omaslack-bin" and the action runs the same visible install command

### Requirement: Documented dependency
The plugin README and its marketplace listing SHALL state that the `omaslack-bin` AUR package is required, with the install command. The marketplace submission SHALL declare `installation.mode: manual` with a note pointing to the package.

#### Scenario: Marketplace listing
- **WHEN** a user opens the plugin's page on the marketplace
- **THEN** the page says that the plugin needs the `omaslack-bin` AUR package and shows how to install it
