# docker-account

[![Release](https://img.shields.io/github/v/release/smartcat999/docker-account?style=flat-square)](https://github.com/smartcat999/docker-account/releases/latest)
![macOS and Linux](https://img.shields.io/badge/macOS%20%7C%20Linux-supported-3b82f6?style=flat-square)
![amd64 and arm64](https://img.shields.io/badge/amd64%20%7C%20arm64-supported-64748b?style=flat-square)

Switch between Docker Hub or private registry accounts without logging in again.

Credentials stay isolated per account while Docker contexts, Buildx builders, and CLI plugins remain shared.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/smartcat999/docker-account/main/install.sh | sh
```

Prebuilt binaries are available for macOS and Linux on amd64 and arm64.

## Quick start

Add an account and securely enter a password or access token:

```bash
docker account add personal --username your-docker-id --login
```

Enable account switching once for your shell:

```bash
# Zsh
echo 'eval "$(docker account env)"' >> ~/.zshrc
source ~/.zshrc

# Bash: use ~/.bashrc instead
```

Open the interactive selector:

```bash
docker account use
```

Or switch directly:

```bash
docker account use personal
docker account current
```

Docker commands in the same shell now use the selected account.

## Commands

| Command | Description |
| --- | --- |
| `docker account add NAME -u USER [--login]` | Add an account and optionally log in |
| `docker account use [NAME]` | Select interactively or switch by name |
| `docker account list` | Show accounts, current selection, and credential status |
| `docker account login NAME` | Save credentials for an account |
| `docker account logout NAME` | Remove an account's saved credentials |
| `docker account remove NAME` | Remove an account definition |
| `docker account shell [NAME]` | Open a child shell using an account |

Run `docker account COMMAND --help` for all options.

## How it works

Account credentials are stored separately under `~/.docker/accounts`. Docker runtime configuration stays shared:

| Isolated per account | Shared with Docker |
| --- | --- |
| Registry credentials | Contexts and daemon endpoints |
| Credential helper namespace | Buildx builders |
| Current account selection | CLI plugins |

On macOS, credentials remain in Keychain with an account-specific namespace. Removing an account does not delete shared Docker contexts or builders.

## Development

Requires Go 1.25+:

```bash
make test
make install
```
