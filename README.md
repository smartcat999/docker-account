# docker-account

A lightweight Docker CLI plugin for managing and switching between multiple Docker Hub or registry accounts.

## Install

Requirements: Docker CLI and Go 1.25+.

```bash
git clone https://github.com/smartcat999/docker-account.git
cd docker-account
make install
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
