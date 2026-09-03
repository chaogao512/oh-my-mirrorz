# Security Policy

## Supported versions

Only the latest tagged release receives security fixes during the v0.x series.

## Reporting a vulnerability

Please do not open a public issue for a suspected vulnerability. Use GitHub's private security advisory reporting for this repository. Include the affected command, platform, minimal reproduction, and whether any configuration or credential may have been exposed.

Do not include real tokens, private mirror URLs, or complete private configuration files. Replace sensitive values with placeholders.

## Security boundaries

The tool never executes commands supplied by mirror metadata. It accepts credential-free HTTPS endpoints by default, does not print configuration contents in plans, stores snapshots with restrictive permissions, and invokes `sudo` only for explicitly requested system-scoped APT writes.
