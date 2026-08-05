# Install the daemon

This guide installs the daemon that owns Zen's state, pairing identity, tmux sessions, and executor processes. The Android or iOS app connects to this daemon; installing a mobile client alone is not enough.

## What you need

- Linux `amd64`/`arm64` or an Apple Silicon Mac
- `curl`, `tar`, and either `sha256sum` (Linux) or `shasum` (macOS)
- `tmux` on `PATH`
- At least one AI CLI on `PATH` and already authenticated (`codex`, `claude`, `cursor-agent`, `grok`, `pi`, or `opencode`)

You do not need every executor. One is enough.

## Install with one command

```sh
curl -fsSL https://raw.githubusercontent.com/daoleno/zen/main/install.sh | sh
```

On every fresh bootstrap run without `ZEN_VERSION`, the installer queries GitHub at run time and selects the SemVer-highest public nondraft Release whose tag matches supported `vX.Y.Z` or `vX.Y.Z-beta.N` syntax. GitHub's `prerelease` flag does not affect eligibility: strict SemVer ordering makes a stable tag outrank a beta at the same core version, while a beta with a higher core version outranks a stable release with a lower core. The default therefore follows today's beta releases and automatically moves to a future stable release without an embedded version that needs routine README or script updates. `ZEN_VERSION` is optional pinning only.

The installer detects the supported platform, downloads the selected release's exact archive and `SHA256SUMS` from the official [`daoleno/zen`](https://github.com/daoleno/zen) release, verifies SHA-256, rejects unexpected archive entries and links, and atomically installs a mode-`0755` executable. It never invokes `sudo`, installs packages, installs AI CLIs, logs in, or sends telemetry.

If a usable Zen executable already exists at a safe user-owned location, the default command runs its built-in `zen update` instead. That updater verifies the signed schema-v2 release manifest and authenticated archive checksum before replacing the executable. Setting `ZEN_VERSION` or `ZEN_INSTALL_DIR` requests a fresh bootstrap install instead:

```sh
# Exact supported tag; no other tag syntax is accepted.
curl -fsSL https://raw.githubusercontent.com/daoleno/zen/main/install.sh | \
  ZEN_VERSION=v0.1.0-beta.11 sh

# Explicit user-owned destination.
curl -fsSL https://raw.githubusercontent.com/daoleno/zen/main/install.sh | \
  ZEN_INSTALL_DIR="$HOME/bin" sh

# Do not edit a shell profile (useful for managed hosts and CI).
curl -fsSL https://raw.githubusercontent.com/daoleno/zen/main/install.sh | \
  ZEN_NO_PATH_UPDATE=1 sh
```

`ZEN_DRY_RUN=1` prints the selected platform, requested version, and destination without downloading or changing files. The installer is noninteractive and uses nonzero exits with specific errors, so version pins and custom destinations work in CI as well.

### Install location and PATH

With no `ZEN_INSTALL_DIR`, the installer preserves a safe existing user-owned Zen location. Otherwise, it selects an existing writable user-owned `bin` directory on `PATH` only when there is exactly one unambiguous choice; the fallback is `~/.local/bin`. It refuses root/system directories and refuses to run as root.

When the fallback is not already on `PATH`, the installer can append one marked, idempotent entry to `.zshrc`, `.bashrc`, or `.config/fish/config.fish`, according to `SHELL`. It will not edit a symlinked or foreign-owned profile. A piped installer cannot modify its parent shell, so the final output prints the exact `export PATH=...` or `fish_add_path ...` command to run immediately and the full installed path to use in the meantime. Set `ZEN_NO_PATH_UPDATE=1` to suppress all profile changes; the immediate command is still printed.

After installation, the script runs a safe `--help` execution check followed by `zen doctor` using the full installed path. Missing `tmux`, an agent CLI, or authentication is reported as host setup guidance rather than a corrupt installation; the installer never tries to remedy those dependencies itself.

### Bootstrap trust boundary

The first `install.sh`, release archive, and `SHA256SUMS` arrive over GitHub HTTPS. The checksum detects a damaged or mismatched archive, but because the checksum is delivered through the same HTTPS trust boundary, the bootstrap does **not** claim Ed25519 authentication. Review the repository-root [`install.sh`](../install.sh), pin its commit in the raw URL if your policy requires reviewed immutable bootstrap code, or use the manual path below.

As optional transport hardening on curl versions that support these flags, require HTTPS for the initial script request:

```sh
curl --proto '=https' --proto-redir '=https' -fsSL https://raw.githubusercontent.com/daoleno/zen/main/install.sh | sh
```

After the first install, `zen update` has a stronger trust path: the installed binary contains Zen's Ed25519 public key and requires the signed schema-v2 manifest before accepting an archive. The bootstrap never downloads remote shell code and pipes it into a second shell.

## Supported platforms

| Host                                        | Archive                   | Status                                                                |
| ------------------------------------------- | ------------------------- | --------------------------------------------------------------------- |
| Linux `amd64` / `x86_64`                    | `zen-linux-amd64.tar.gz`  | Supported                                                             |
| Linux `arm64` / `aarch64`                   | `zen-linux-arm64.tar.gz`  | Supported                                                             |
| WSL2 on either supported Linux architecture | matching Linux archive    | Supported                                                             |
| Apple Silicon macOS                         | `zen-darwin-arm64.tar.gz` | Supported                                                             |
| Intel macOS                                 | —                         | Unsupported; build from source if you are evaluating an untested host |
| Native Windows                              | —                         | Unsupported; use WSL2 or build from source for development            |

The installer fails on unsupported platforms before downloading an archive. It does not present native Windows or Intel macOS as release-supported hosts.

## Manual release download and checksum verification

Open [GitHub Releases](https://github.com/daoleno/zen/releases) and download:

- `zen-linux-amd64.tar.gz` for most Intel/AMD Linux machines
- `zen-linux-arm64.tar.gz` for 64-bit ARM Linux machines
- `zen-darwin-arm64.tar.gz` for Apple Silicon Macs
- `SHA256SUMS` to verify the download

```bash
# Linux
grep 'zen-linux-amd64.tar.gz$' SHA256SUMS | sha256sum -c -

# macOS
grep 'zen-darwin-arm64.tar.gz$' SHA256SUMS | shasum -a 256 -c -

tar -xzf zen-<platform>-<architecture>.tar.gz
mkdir -p ~/.local/bin
install -m 755 zen ~/.local/bin/zen
~/.local/bin/zen --help
~/.local/bin/zen doctor
```

Replace the Linux archive name with `zen-linux-arm64.tar.gz` on ARM64. Before accepting the checksum line, inspect it and confirm there is exactly one entry for the archive you downloaded. Manual installation has the same initial GitHub HTTPS trust boundary described above.

After the first install, Zen can update itself in place:

```bash
zen update --check
zen update
```

`zen update` selects the supported archive for the current OS and architecture, verifies the release's signed manifest and archive checksum, then atomically replaces the current user-owned executable. It never uses `sudo` or restarts Zen. If the daemon is running, stop and start it when convenient to use the new binary.

An interactive daemon startup may print one cached `zen update` hint. The check runs asynchronously, never delays startup, and stays silent in noninteractive output and on network failure.

On macOS, install `tmux` separately if needed. If macOS blocks the downloaded binary, confirm that it came from the official Zen release before removing the quarantine attribute with `xattr -d com.apple.quarantine ~/.local/bin/zen`.

If `~/.local/bin` is not on your `PATH`, install into another user-owned directory that is, or add it to your shell configuration.

## Build from source

Source builds require the Go toolchain declared in `daemon/go.mod`:

```bash
git clone https://github.com/daoleno/zen.git
cd zen/daemon
go build -o bin/zen ./cmd/zen/
./bin/zen --help
```

Product version for banners and release staging comes from `app/app.base.json` (`expo.version`). The daemon default is `daemon/cmd/zen/version.go` and can be overridden at link time (`-X main.Version=…`).

## Release binaries (Linux and Apple Silicon macOS)

Cross-build without CGO (deterministic flags: `-trimpath`, `-buildvcs=false`, stripped ldflags):

```bash
./scripts/build-daemon-linux.sh
# → dist-download/staging/bin/zen-linux-amd64
# → dist-download/staging/bin/zen-linux-arm64
# → dist-download/staging/bin/zen-darwin-arm64
```

Full local stage (clean directory each run; **no** GitHub Release):

```bash
./scripts/stage-release.sh
# → dist-download/vVERSION/
#    zen-linux-amd64.tar.gz
#    zen-linux-arm64.tar.gz
#    zen-darwin-arm64.tar.gz
#    SHA256SUMS  release-manifest.json  release-manifest.json.sig
```

Each archive contains the `zen` binary plus `LICENSE`, `NOTICE`, and `TRADEMARKS.md`. The command prints the exact stage path; tracked release notes remain on the GitHub Release page instead of becoming duplicate download assets.

Release staging requires the Ed25519 manifest key through `ZEN_UPDATE_SIGNING_KEY` (a local PEM path) or `ZEN_UPDATE_SIGNING_KEY_BASE64` (CI). The committed public key is the trust root embedded in the daemon; private key material is never stored in Git.

Verify identity sources and stage checksums:

```bash
./scripts/verify-release-identity.sh
VERSION="$(python3 -c "import json; print(json.load(open('app/app.base.json'))['expo']['version'])")"
./scripts/verify-release-identity.sh --stage "dist-download/v$VERSION"
(cd "dist-download/v$VERSION" && sha256sum -c SHA256SUMS)
```

## Install via Go modules (optional)

```bash
go install github.com/daoleno/zen/daemon/cmd/zen@latest
zen
```

This path depends on the published module and Go proxy state. If `@latest` fails, build from a clone as above.

## Run as a background service

The beta does not yet install a system service automatically. Start `zen` in a persistent shell, tmux session, or a user service you manage. The same host user must be able to access `tmux`, your repositories, and the authenticated AI CLI.

Do not run the daemon as root merely to keep it alive.

## Defaults

| Setting                  | Value                                                                  |
| ------------------------ | ---------------------------------------------------------------------- |
| Listen                   | `127.0.0.1:9876`                                                       |
| State directory          | `~/.zen`                                                               |
| Work log                 | `~/.zen/work`                                                          |
| Executors file           | `~/.zen/executors.toml` (optional; built-in defaults apply if missing) |
| Brain data               | `~/.zen/brain`                                                         |
| Optional Zen Link config | `~/.zen/link.json`                                                     |

To keep a custom location:

```bash
zen -state-dir /path/to/state
zen pair -state-dir /path/to/state https://your-host.example
```

Use the same `-state-dir` for the daemon and for `zen pair`.

## Starting and pairing are separate

Choose one connectivity route:

1. **Direct private network:** run `zen --lan` on the computer while the phone
   is on the same trusted Wi-Fi or Tailnet. Zen listens on `0.0.0.0:9876` and
   prints usable pair commands for detected private addresses. Run one in
   another terminal; never pair with `0.0.0.0`.
2. **HTTPS endpoint:** run bare `zen` on its secure loopback default, forward
   the full `http://127.0.0.1:9876` origin through an HTTPS ingress, then run
   `zen pair https://your-zen-host.example` in another terminal.
3. **Optional Zen Link:** after an operator explicitly configures
   `~/.zen/link.json` and relay infrastructure, run bare `zen`, then run
   `zen pair` in another terminal. Link keeps the daemon loopback-only and
   opens outbound connections. This repository does not configure or start it;
   see [Zen Link Relay operations](zen-link-relay.md).

`-addr` remains available for an advanced explicit bind. It cannot be combined with `--lan`.

There is no `-advertise-url` flag. Pairing V1 receives its external origin at
pair time; Pairing V2 receives candidates from explicit Link config.

## Keep it running

For a personal beta, a tmux pane or systemd user unit is enough. Example user unit (adjust paths):

```ini
[Unit]
Description=zen agent control plane
After=network.target

[Service]
ExecStart=%h/bin/zen -addr 127.0.0.1:9876 -state-dir %h/.zen
Restart=on-failure

[Install]
WantedBy=default.target
```

## Docker (advanced)

See `daemon/Dockerfile`. Prefer the host binary so tmux and local agent CLIs share the same environment. Container use requires mounting state, publishing or proxying port 9876, and arranging access to host agent tools yourself.

## Diagnostics

```bash
zen doctor
zen doctor --json
```

`zen doctor` reports whether tmux, the state directory, the listen address, and executors look ready. It does not install packages or print credentials.

Guided config write:

```bash
zen setup
# or automation:
zen setup --non-interactive --host codex --delegated codex --profile safe
```

`zen setup` stops cleanly with install hints when tmux/state-dir block readiness. Restart the daemon after it writes `~/.zen/executors.toml` so new executor definitions load; once running, switch only the Delegated Executor with `zen brain set-delegated <id>` without restart.
