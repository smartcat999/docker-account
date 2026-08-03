# Security policy

## Supported versions

Security updates are provided for the latest release.

## Reporting a vulnerability

Please use GitHub's **Report a vulnerability** form in the repository Security tab. Do not include passwords, access tokens, Keychain contents, or an unredacted Docker configuration in a public issue.

Include the affected version, operating system, reproduction steps, and the security impact. You can expect an initial response within seven days.

## Credential model

- Registry credentials are isolated under `~/.docker/accounts`.
- On macOS, secrets remain in Keychain under account-specific namespaces.
- Docker contexts and Buildx state are shared with the user's normal Docker configuration.
- `docker account logout` removes saved credentials; `remove` deletes account-local configuration without deleting shared contexts or builders.
