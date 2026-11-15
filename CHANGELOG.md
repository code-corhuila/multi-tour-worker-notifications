# Changelog

All notable changes of `multi-tour-worker-notifications` (Business e-mail worker) are documented in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [3.0.0] - 2026-11-14

Cut 3 release. Every entry below is a pull request merged into `qa` and re-applied to `release/3.0.0` with `git cherry-pick -x`; the heading of each group is the user story named in the pull request, and the entry is the title of the pull request without its type. The frozen head of `qa` is `12e19c16e8b7d17f3ff5ade8308b02cb6251a110`.

### Changed

#### HU-WRK-004 - Send business e-mail

- The skeleton of the notifications worker (part 1) ([PR 10](https://github.com/code-corhuila/multi-tour-worker-notifications/pull/10))
- The password reset notice (part 2) ([PR 11](https://github.com/code-corhuila/multi-tour-worker-notifications/pull/11))
- The event routes of job 7 (part 3) ([PR 12](https://github.com/code-corhuila/multi-tour-worker-notifications/pull/12))
- The SMTP sending and the emergency notice e-mail (part 4) ([PR 13](https://github.com/code-corhuila/multi-tour-worker-notifications/pull/13))
- The customer notice e-mail of HU-COM-015 (part 5) ([PR 14](https://github.com/code-corhuila/multi-tour-worker-notifications/pull/14))
- Send the failed password reset notice ([PR 16](https://github.com/code-corhuila/multi-tour-worker-notifications/pull/16))
- Pin the worker runtime at its first version v0.1.0 ([PR 18](https://github.com/code-corhuila/multi-tour-worker-notifications/pull/18))

[Unreleased]: https://github.com/code-corhuila/multi-tour-worker-notifications/compare/v3.0.0...HEAD
[3.0.0]: https://github.com/code-corhuila/multi-tour-worker-notifications/releases/tag/v3.0.0
