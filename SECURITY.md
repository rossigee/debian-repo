# Security Policy

## Reporting a vulnerability

**Do not open a public issue for a security vulnerability.**

This repository has [private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)
enabled. Use **Security → Report a vulnerability** on this repository, which opens a
private advisory visible only to the maintainer.

Please include:

- what the vulnerability allows an attacker to do
- the affected version, commit, or file
- reproduction steps or a proof of concept
- any suggested remediation

Expect an acknowledgement within a few days. There is no formal SLA; if you need one,
say so in the report and it will be addressed explicitly.

## If you are part of the team

Almost everything here is tracked in public: roadmap, design discussions, issues, and
the plan for the v1.0 authorization rewrite. **A live vulnerability in released code is
the exception.** If you find one, it goes through private vulnerability reporting like
any other report, not the public board.

This is worth stating because the pressure runs the other way. Every other kind of work
in this repository is visible to everyone by the time it is discussed, and a public
issue is the fast, natural way to pull people in. For a vulnerability that is the wrong
instinct even where no sensitive data is involved, because:

- The advisory is the coordination point. A public issue fragments the discussion,
  loses the confidentiality that `Valid-Until`-style disclosure windows depend on, and
  is trivially discoverable by anyone watching the repository.
- A fix branch and a pull request are public the moment they are pushed. Opening the
  issue first publishes the shape of the flaw before the patch exists.

This applies to bugs in **released** code. A flaw in an unreleased design is not a
vulnerability in anything a user can run, and discussing it openly — including the
alternative approaches that were rejected — is one of the better reasons for the project
being open source. Design discussion belongs in public; vulnerability disclosure does
not.

If you are unsure which category something is, it is a vulnerability. Open a private
advisory and say so in it; it can be reclassified in public afterwards.

## Scope

`debian-repo` is a Go service that serves a Debian package repository and accepts
authenticated package uploads. The interesting attack surfaces are:

| Surface | Notes |
|---|---|
| Upload / register / presign | CI-authenticated, accepts attacker-controlled `.deb` archives |
| `.deb` control-file parsing | `internal/validate` — parses untrusted archives |
| `Packages` / `Release` rendering | content originates from uploaded control fields |
| HTTP auth middleware | bearer tokens (CI), HTTP Basic (apt), OIDC sessions (web) |
| MinIO object storage | path construction from user-supplied suite/component names |
| GPG signing | private key handling, passphrase and key derivation |
| `repoctl` | local CLI, filesystem-authorised only |

In scope: anything reachable over the network from a hosted deployment, and any
privilege escalation between tenants once multi-tenancy lands in v1.0.

Out of scope: vulnerabilities requiring an attacker to already hold a valid CI bearer
token with the relevant grant, or local root on the host.

## Deployment assumptions

This service is designed to run inside a private network, behind a TLS-terminating
reverse proxy (haproxy in the reference deployment). It is **not** hardened for direct
exposure to the public internet:

- it does not terminate TLS itself
- the metrics endpoint is bearer-token protected but not network-restricted
- the reference configuration runs with the `proxy` pool serve mode, not `passthrough`

If you are exposing it publicly, front it with a reverse proxy that enforces TLS and
restricts `/metrics` to your monitoring network.

## Supported versions

| Version | Supported |
|---|---|
| `master` | yes |
| latest tagged release | yes |
| anything older | no |

Fixes land on `master` first and reach a tagged release through the normal
version-bump and tag flow. See [`ROADMAP.md`](ROADMAP.md) for what is in progress.

## Secrets in this repository

The repository has a history of build tooling that materialised a private key on disk
and briefly committed a test key, so a small number of objects from early history
remain reachable in the public repository by SHA. No live credentials were ever
committed.

Two controls now exist to prevent recurrence:

- **secret scanning push protection** is enabled, which blocks pushes containing
  recognised credential patterns
- the `lint` workflow fails a build that adds any `.asc`, `.gpg`, `.key` or `.pem`
  file, or that greps as containing a known secret variable name

If you need to rotate anything because of a disclosure, contact the maintainer
directly rather than opening an issue.

## Signing

Repository signing keys are **never** committed. Tests generate a throwaway GPG key per
run via `internal/testsupport/gpgtest`. Commits to `master` are GPG-signed by the
maintainer's hardware key, which is not present on any CI runner.
