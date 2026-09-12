# Third-party notices and corresponding source

This module embeds the pinned **moonlight-common-c** C core and its dependencies
in the Zen remote-desktop native library (`libzen_moonlight.so`).

## Pinned components

| Component | Pinned revision | License | Notice file |
| --- | --- | --- | --- |
| moonlight-common-c | `62e066388f1a1b133e0bee947b9a374311a3354b` | GPL-3.0 | `notices/GPL-3.0-moonlight-common-c.txt` |
| enet (cgutman fork) | `aca87840b57f045a1f7f9299e4b1b9b8e2a5e2f1` | MIT | `notices/ENET-MIT.txt` |
| nanors | `b1e3c22ca0cdc0bb83e3cd6ed1a2fc77869ed99a` | MIT | `notices/NANORS-MIT.txt` |
| OpenSSL (Android prefix) | `3.5.4` (maintained LTS; 3.0.x EOL 2026-09) | Apache-2.0 | `notices/OPENSSL-APACHE-2.0.txt` |
| Sunshine (host, separate process) | `dd7a1f796e69283a42663630ecd49b174b070778` | GPL-3.0 | distributed with the host package, not embedded in the app |

Exact archive URLs and sha256 digests live in `native.lock.json`. Nothing is
downloaded as an opaque prebuilt binary; the Android library is compiled from
these pinned sources at build time.

## GPL-3.0 corresponding source

moonlight-common-c is licensed under GPL-3.0. Zen complies by:

1. shipping the exact upstream source revision, unmodified, obtained by
   `scripts/fetch-moonlight-common-c.sh` (hash-verified against `native.lock.json`);
2. shipping the build recipe that compiles it (`android/CMakeLists.txt`,
   `scripts/build-openssl-android.sh`);
3. keeping the license text in `notices/GPL-3.0-moonlight-common-c.txt`.

No upstream source file is patched. `-Werror` is dropped from the CMake target
options at configure time so toolchain-only warnings do not break the build;
this changes compiler flags for the build, not the source.

Corresponding source for a published binary is therefore:
`https://codeload.github.com/moonlight-stream/moonlight-common-c/tar.gz/<pin>`
plus `enet` and `nanors` at their pinned revisions, plus the unmodified build
files in this directory.

## Reproducible build

```sh
# 1. Fetch and verify pinned client sources.
scripts/fetch-moonlight-common-c.sh

# 2. Android: build the pinned OpenSSL prefix for each ABI (once per ABI).
#    API level follows the module minSdk (default 24).
scripts/build-openssl-android.sh --abi arm64-v8a
scripts/build-openssl-android.sh --abi x86_64

# 3. Build the module (CMake 3.22.1 + NDK 27.1.12297006).
cd app/android && ./gradlew :zen-remote-desktop:assembleDebug -PreactNativeArchitectures=arm64-v8a
```

Bridge lifecycle tests (host JVM, inert core, no network/host):

```sh
ZEN_MOONLIGHT_BRIDGE_TEST=1 ./gradlew :zen-remote-desktop:testDebugUnitTest
# or directly:
scripts/build-moonlight-bridge-test.sh
```

A clean checkout has no `third_party/`; CMake fails with the exact fetch
command instead of building against stale local state.

Host compile check (no device, no Android SDK required):

```sh
cmake -S app/modules/zen-remote-desktop/android -B /tmp/zen-moonlight-host \
  -DZEN_HOST_BUILD=ON -DCMAKE_BUILD_TYPE=Release
cmake --build /tmp/zen-moonlight-host -j2
```

## Modifications

None to upstream sources. Zen additions are limited to:

- `android/src/main/cpp/moonlight_bridge.c` (JNI translation layer);
- `android/CMakeLists.txt` (build wiring);
- `android/src/main/java/expo/modules/zenremotedesktop/MoonlightCore.kt`
  (Kotlin lifecycle/input contract);
- `scripts/fetch-moonlight-common-c.sh`, `scripts/build-openssl-android.sh`.

Pairing, certificate pinning, and RTSP/control handling are **not** implemented
in the bridge; they remain upstream app-layer responsibilities that must be
ported from the reference clients before a session can start against a real
Sunshine host.
