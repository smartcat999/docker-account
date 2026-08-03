# Troubleshooting

Start with the built-in diagnostic command:

```bash
docker account doctor
```

## `docker: 'account' is not a docker command`

Reinstall the plugin and verify that it is executable:

```bash
curl -fsSL https://raw.githubusercontent.com/smartcat999/docker-account/main/install.sh | sh
docker account version
```

## Switching does not affect Docker commands

Activate the stable account configuration once in the current shell:

```bash
eval "$(docker account env)"
```

Add the same line to `~/.zshrc` or `~/.bashrc` to activate it in new shells.

## Docker daemon or Buildx cannot connect

Check the inherited context and active builder:

```bash
docker context show
docker buildx inspect
```

Select a working context or builder using the normal Docker commands. Account switching shares this runtime state and does not create a Docker daemon.

## Login fails on macOS

Confirm that Docker's native Keychain helper is available:

```bash
command -v docker-credential-osxkeychain
```

Do not post Keychain contents, passwords, tokens, or an unredacted `config.json` in an issue.
