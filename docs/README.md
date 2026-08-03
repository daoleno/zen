# Zen documentation

This documentation is organized by what you are trying to accomplish. If you only want to use Zen, start with the first three guides; architecture and release documents are for contributors and maintainers.

## Get Zen running

1. [Install or upgrade the daemon](install-daemon.md)
2. [Connect the daemon and pair your phone](connect-and-pair.md)
3. [Configure an AI executor](executors.md)
4. Install or build a mobile client:
   - [Android app](android.md)
   - [iOS app](ios.md)

If something does not work, run `zen doctor` on the host and continue with [Troubleshooting](troubleshooting.md).

## Understand the product

- [Security and privacy](security-and-privacy.md) explains keys, pairing, exposed routes, local data, and executor risk.
- [Architecture](architecture.md) explains the app, daemon, network, tmux, and provider boundaries.
- [Optional Zen Link Relay operations](zen-link-relay.md) covers the inert-by-default single-region relay source, explicit connector config, local E2E, limits, upgrade, and rollback. No Link service is deployed by this repository.
- [Brain orchestration](brain-orchestration.md) explains Work, Event, Session, executor ownership, and the Active work operator surface.
- [Notifications](notifications.md) explains the current notification model.

## Development and maintenance

- [Contributing](../CONTRIBUTING.md)
- [Android native terminal and ABI contract](android.md#architecture--abi-contract)
- [iOS source build and Ghostty XCFramework contract](ios.md#native-terminal--xcframework-contract)
- [iOS CI, signing, and TestFlight automation](ios-ci-release.md)
- [Agent-native task model](agent-native-task-model.md)
- [CI release pipeline](ci-release.md)
- [Versioned release notes](releases/)
- [Third-party assets and licenses](third-party-assets.md)
- [Known release blockers](release-blockers.md)

Design explorations and implementation notes may also live in this directory. They are not part of the installation path unless linked from one of the guides above.
