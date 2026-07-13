# Install the daemon

This guide installs the daemon that owns Zen's state, pairing identity, tmux sessions, and executor processes. The Android or iOS app connects to this daemon; installing a mobile client alone is not enough.

## What you need

- Linux `amd64`/`arm64` or an Apple Silicon Mac
- `tmux` on `PATH`
- At least one AI CLI on `PATH` and already authenticated (`codex`, `claude`, `cursor-agent`, or `grok`)

You do not need every executor. One is enough.

## Install the release binary

Open [GitHub Releases](https://github.com/daoleno/zen/releases) and download:

- `zen-linux-amd64.tar.gz` for most Intel/AMD Linux machines
- `zen-linux-arm64.tar.gz` for 64-bit ARM Linux machines
- `zen-darwin-arm64.tar.gz` for Apple Silicon Macs
- `SHA256SUMS` to verify the download

```bash
# Linux
sha256sum -c SHA256SUMS --ignore-missing

# macOS
grep 'zen-darwin-arm64.tar.gz$' SHA256SUMS | shasum -a 256 -c -

tar -xzf zen-<platform>-<architecture>.tar.gz
install -m 755 zen ~/.local/bin/zen
zen doctor
```

On macOS, install `tmux` with `brew install tmux`. If macOS blocks the downloaded binary, confirm that it came from the official Zen release before removing the quarantine attribute with `xattr -d com.apple.quarantine ~/.local/bin/zen`.

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
#    SHA256SUMS  release-manifest.json
```

Each archive contains the `zen` binary plus `LICENSE`, `NOTICE`, and `TRADEMARKS.md`. The command prints the exact stage path; tracked release notes remain on the GitHub Release page instead of becoming duplicate download assets.

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

| Setting | Value |
| --- | --- |
| Listen | `127.0.0.1:9876` |
| State directory | `~/.zen` |
| Work log | `~/.zen/work` |
| Executors file | `~/.zen/executors.toml` (optional; built-in defaults apply if missing) |
| Brain data | `~/.zen/brain` |

To keep a custom location:

```bash
zen -state-dir /path/to/state
zen pair -state-dir /path/to/state https://your-host.example
```

Use the same `-state-dir` for the daemon and for `zen pair`.

## Starting and pairing are separate

Choose one connectivity route:

1. **Direct private network:** run `zen --lan` on the computer while the phone is on the same trusted Wi-Fi or Tailnet. Zen listens on `0.0.0.0:9876` and prints usable pair commands for detected private addresses. Run one of those commands in another terminal; never pair with `0.0.0.0`.
2. **HTTPS endpoint:** run bare `zen` on its secure loopback default, forward the full `http://127.0.0.1:9876` origin through an HTTPS ingress, then run `zen pair https://your-zen-host.example` in another terminal.

`-addr` remains available for an advanced explicit bind. It cannot be combined with `--lan`.

There is no `-advertise-url` flag. The externally reachable origin is supplied at pair time.

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

`zen setup` stops cleanly with install hints when tmux/state-dir block readiness. Restart the daemon after it writes `~/.zen/executors.toml`.
