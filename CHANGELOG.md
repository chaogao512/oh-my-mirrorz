# Changelog

All notable changes to this project will be documented in this file.

## [0.2.0] - 2026-09-04

### Added

- Add safe user-level mirror management for Conda, Mamba, and Micromamba while preserving channel policy, private sources, credentials, and unrelated `.condarc` settings.
- Add one protocol-aware benchmark engine shared by every adapter, with candidate names, observed redirect targets, median latency, success counts, and per-capability sample winners.
- Add `omm benchmark --adapter NAME --runs N` filtering and repeat controls.
- Add protocol-aware preflight checks before any configuration write.

### Changed

- Make `prefer` fall back to `auto` when the named mirror fails preflight, not only when it is absent from the catalog.
- Keep APT mirrorlist probes non-rankable so client-side multi-mirror failover is not replaced by a misleading one-shot winner.
- Update the client user agent to the v0.2 protocol level.

## [0.1.1] - 2026-09-03

### Fixed

- Make the Homebrew opt-in test independent of whether `brew` is installed on the CI host.
- Move GitHub Actions to Node 24-based checkout and Go setup releases.

## [0.1.0] - 2026-09-03

### Added

- Safe mirror switching for pip/uv, npm, Cargo, Homebrew, and Debian/Ubuntu APT.
- Auto, fixed, and preferred mirror strategies.
- Dry-run plans, snapshots, transaction history, verification, rollback, and undoable restore.
- Explicit, narrowly scoped privilege escalation for system APT files.
- macOS/Linux amd64 and arm64 release builds.
