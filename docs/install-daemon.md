# Install the daemon

This guide installs the Linux daemon that owns Zen's state, pairing identity, tmux sessions, and executor processes. The Android app connects to this daemon; installing the APK alone is not enough.

## What you need

- Linux `amd64` or `arm64` host
- `tmux` on `PATH`
- At least one AI CLI on `PATH` and already authenticated (`codex`, `claude`, `cursor-agent`, or `grok`)

You do not need every executor. One is enough.

## Install the release binary

Download the files for `v0.1.0-beta.1` from the [GitHub release](https://github.com/daoleno/zen/releases/tag/v0.1.0-beta.1):

- `zen-linux-amd64` for most Intel/AMD Linux machines
- `zen-linux-arm64` for 64-bit ARM Linux machines
- `SHA256SUMS` to verify the download

```bash
sha256sum -c SHA256SUMS --ignore-missing
chmod +x zen-linux-*
install -m 755 zen-linux-* ~/.local/bin/zen
zen doctor
zen
```

If `~/.local/bin` is not on your `PATH`, install into another user-owned directory that is, or add it to your shell configuration.

## Build from source

Source builds require the Go toolchain declared in `daemon/go.mod`:

```bash
git clone https://github.com/daoleno/zen.git
cd zen/daemon
go build -o bin/zen ./cmd/zen/
./bin/zen
```

Product version for banners and release staging matches `app/app.base.json` (`expo.version`, currently `0.1.0-beta.1`). The daemon default is `daemon/cmd/zen/version.go` and can be overridden at link time (`-X main.Version=…`).

## Linux release binaries (amd64 / arm64)

Cross-build without CGO (deterministic flags: `-trimpath`, `-buildvcs=false`, stripped ldflags):

```bash
./scripts/build-daemon-linux.sh
# → dist-download/staging/bin/zen-linux-amd64
# → dist-download/staging/bin/zen-linux-arm64
```

Full local stage (clean directory each run; **no** GitHub Release):

```bash
./scripts/stage-release.sh
# → dist-download/v0.1.0-beta.1/
#    zen-linux-amd64
#    zen-linux-arm64
#    LICENSE  NOTICE  TRADEMARKS.md  GHOSTTY-MIT.txt
#    RELEASE_NOTES.md  (from docs/releases/v0.1.0-beta.1.md)
#    SHA256SUMS  identity.json
```

Binaries are **top-level** (GitHub Release-facing names), not under `bin/`. Tracked notes live in `docs/releases/v0.1.0-beta.1.md`.

Verify identity sources and stage checksums:

```bash
./scripts/verify-release-identity.sh
./scripts/verify-release-identity.sh --stage dist-download/v0.1.0-beta.1
(cd dist-download/v0.1.0-beta.1 && sha256sum -c SHA256SUMS)
```

## Install via Go modules (optional)

```bash
go install github.com/daoleno/zen/daemon/cmd/zen@latest
zen
```

This path depends on the published module and Go proxy state. If `@latest` fails, build from a clone as above.

## Run as a background service

The beta does not yet install a system service automatically. Start `zen` in a persistent shell, tmux session, or a user service you manage. The same Linux user must be able to access `tmux`, your repositories, and the authenticated AI CLI.

Do not run the daemon as root merely to keep it alive.

## Defaults

| Setting | Value |
| --- | --- |
| Listen | `127.0.0.1:9876` |
| State directory | `~/.zen` |
| Work log | `~/.zen/work` |
| Executors file | `~/.zen/executors.toml` (optional; built-in defaults apply if missing) |
| Brain data | `~/.zen/brain` |

### Legacy state directory

Older installs used `~/.config/zen`. On first start with the default state dir, zen **copies** `identity.json`, `trusted-devices.json`, and `pairing-tokens.json` into `~/.zen` if the new files are missing. It does not delete the legacy directory.

To keep a custom location:

```bash
zen -state-dir /path/to/state
zen pair -state-dir /path/to/state https://your-host.example
```

Use the same `-state-dir` for the daemon and for `zen pair`.

## Starting and pairing are separate

1. Start `zen` so it has a stable identity and listening address.
2. Expose `http://127.0.0.1:9876` through your network layer.
3. Run `zen pair <full-origin>` to print a one-time link/QR without restarting.

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
