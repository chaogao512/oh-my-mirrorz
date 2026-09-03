# Changelog

All notable changes to this project will be documented in this file.

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
