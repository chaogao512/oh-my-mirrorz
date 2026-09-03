# oh-my-mirrorz

A safe, reviewable, and reversible mirror switcher for macOS and Linux.

`oh-my-mirrorz` scans installed package ecosystems, plans repository-specific mirror changes, snapshots the original files, applies changes, and verifies both configuration and connectivity. Any verification failure rolls back the files already changed.

> This is an independent community project. It is not an official MirrorZ or CERNET client and is not endorsed by any mirror operator.

Supported in v0.1.0: pip/uv, npm, Cargo, Homebrew API/bottles/build-time PyPI, and Debian/Ubuntu APT. APT is excluded unless `--system` is explicitly supplied. Security repositories and third-party APT repositories are preserved by default. Homebrew Git remotes remain untouched so a file snapshot can fully restore every applied change.

## Quick start

```bash
omm scan
omm switch --dry-run
omm switch
omm history
omm restore
omm doctor
```

Select adapters or a fixed/preferred mirror:

```bash
omm switch --only pip,npm,cargo
omm switch --exclude homebrew
omm switch --strategy fixed --mirror tuna
omm switch --prefer ustc
omm switch --system
```

See the [Chinese README](README.md) for the full safety model, installation instructions, and command reference.

## Build

Go 1.26 or newer is required.

```bash
go test ./...
go build -trimpath -o omm ./cmd/omm
```

Licensed under the [MIT License](LICENSE).
