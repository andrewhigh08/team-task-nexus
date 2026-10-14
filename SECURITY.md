# Security policy

## Supported versions

| Version | Supported |
|---------|-----------|
| 0.1.x | yes |
| older | no |

Fixes land on `main` and in the next release of the supported line.

## Reporting a vulnerability

Please do not open a public issue for security problems. Report them privately through GitHub
security advisories: https://github.com/andrewhigh08/team-task-nexus/security/advisories/new.
Include a description of the issue, the affected endpoint or component and, if possible, steps to
reproduce. You will get an acknowledgement within a few days.

## Scope

The service is a REST API for team task management. Its access model is described in
[docs/adr](docs/adr) (ADR 0002 in particular) and in the README. Reports about the following are
especially welcome:

- access to teams, tasks, history or comments by a user who is not a member of the team, or any way
  to tell a foreign team or task from a missing one;
- actions beyond a member's role (adding admins, removing members, reading orphaned assignments);
- token validation (algorithm, issuer, expiry, subject);
- rate limit bypass on registration, login or the authenticated API;
- leakage of secrets or personal data in logs, error responses, webhook deliveries or the image.

Known limitations that are not vulnerabilities by themselves, since they are documented design
decisions: access tokens cannot be revoked before they expire; the rate limiter lets requests through
while Redis is unavailable; `/metrics`, `/health/*` and `/openapi.yaml` are served without
authentication; outbox events are delivered at least once and the webhook request is not signed;
registration and adding a team member by email reveal whether an email is registered, and members are
added without an invitation.
