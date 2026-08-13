# Security Policy

## Supported versions

Only the latest release receives security fixes. Releases are published at
[Releases](https://github.com/MaimoryLab/BootAgent/releases); the newest tag is the
supported one.

## Reporting a vulnerability

Report privately through
[GitHub Security Advisories](https://github.com/MaimoryLab/BootAgent/security/advisories/new).
That keeps the report visible only to maintainers until a fix is available.

Please do not open a public issue for a vulnerability. If you cannot use Security
Advisories, open an issue containing only that you have a report to make and no
technical detail, and a maintainer will arrange a private channel.

Include what you have: affected version, platform, the steps you took, and what you
observed. A partial report is worth sending — do not wait until you have a full
exploit.

Expect an acknowledgement within 7 days. Fix timing depends on severity and on
whether the cause is in BootAgent or in a dependency, and you will be told which.

## Scope

BootAgent stores API keys in private local configuration and writes configuration
files for the Agents it manages. Reports that matter most:

- API keys reaching anywhere they should not: logs, error messages, ordinary
  configuration files, status payloads, URLs, or exported settings files
- Configuration writes escaping their intended path, or destroying a file the tool
  did not create
- Update verification being bypassed: an artifact installed without a matching
  SHA-256 digest, or one that replaces the application with something that is not
  an application
- Installer or launch paths executing something the user did not choose,
  including through a crafted Provider entry, Profile, or imported settings file

Out of scope:

- Vulnerabilities in the Agents BootAgent installs (Claude Code, Codex, and the
  others). Report those to their own projects; if BootAgent's handling makes such a
  bug reachable when it otherwise would not be, that part is in scope.
- Vulnerabilities in a Provider's API. Report those to the Provider.
- An API key readable by someone who already has your user account or your
  unlocked machine. Local configuration is protected against other users, not
  against the account that owns it.

## Handling of secrets in reports

Do not include a real API key in a report. If reproducing needs one, say so and a
maintainer will use their own. If you have already sent a real key, revoke it at the
Provider — treat it as disclosed regardless of who has seen it.
