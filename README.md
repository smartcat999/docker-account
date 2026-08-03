# docker-account

A lightweight Docker CLI plugin for managing and switching between multiple Docker Hub or registry accounts.

Only credentials are isolated; Docker contexts, Buildx builders, and CLI plugins stay shared.

## Install

macOS or Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/smartcat999/docker-account/main/install.sh | sh
docker account version
```

## Quick start

Add an account and log in with a password or Docker Hub access token:

```bash
docker account add personal --username your-docker-id --login
```

Enable switching once in Zsh:

```bash
echo 'eval "$(docker account env)"' >> ~/.zshrc
source ~/.zshrc
```

Select interactively with the arrow keys:

```bash
docker account use
```

Or switch directly:

```bash
docker account use personal
```

## Commands

```bash
docker account list
docker account current
docker account login NAME
docker account logout NAME
docker account remove NAME
```

Run `docker account COMMAND --help` for command-specific options.

Account configs are isolated under `~/.docker/accounts`. On macOS, credentials remain in Keychain with an account-specific namespace, so accounts for the same registry do not overwrite each other.

## Development

Requires Go 1.25+:

```bash
make test
make install
```
