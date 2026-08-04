# Account model for v0.9

## Model

Every saved credential is a profile with a name, username, and registry. Each registry independently has one active profile.

```json
{
  "version": 2,
  "current": "default",
  "active": {
    "docker.io": "default",
    "harbor.wuxs.vip": "harbor-admin"
  }
}
```

`current` retains compatibility with existing shell integration and records the most recently selected profile. Credential resolution uses `active`, so switching one registry never changes another.

Registries with one profile are activated automatically. Registries with multiple profiles require an explicit selection.

## Interactive switching

When multiple registries have switchable profiles, `docker account use` starts with the registry:

```text
┌─ Choose a registry ──────────────────────────────┐
│ > Docker Hub              default ●             │
│   harbor.wuxs.vip         harbor-admin ●        │
├──────────────────────────────────────────────────┤
│ ↑↓ move   enter choose   q quit                  │
└──────────────────────────────────────────────────┘
```

The second level shows only profiles for that registry:

```text
┌─ Docker Hub ─────────────────────────────────────┐
│ > default ●               2030047311             │
│   smartcat999             smartcat99999          │
├──────────────────────────────────────────────────┤
│ ↑↓ move   enter switch   ← back   q quit         │
└──────────────────────────────────────────────────┘
```

If only one registry has multiple profiles, the first level is skipped. Registries with only one profile do not appear in the switcher because there is no choice to make.

Direct commands remain available:

```text
docker account use PROFILE
docker account use --registry REGISTRY
docker account current REGISTRY
docker account current --all
```

## Credential resolution

Native credentials keep the existing isolated keys:

```text
System credential:  REGISTRY
Managed credential: REGISTRY/.docker-account/PROFILE
```

For every Docker credential request:

1. Normalize the requested registry, including Docker Hub aliases.
2. Resolve its profile from `state.json.active`.
3. Load that profile's native credential helper.
4. Delegate using the profile-specific namespaced key.

Independent multi-registry resolution requires a native Docker credential helper. Systems without one keep the existing single-registry inline-auth fallback rather than introducing a separate plaintext credential backend.

`DOCKER_ACCOUNT_NAME` overrides only the matching registry. Other registries still resolve through their own active profiles, including inside `docker account shell PROFILE`.

## Compatibility and migration

Existing v0.8 state contains only `current`. It is migrated in memory as follows:

- The current profile becomes active for its registry.
- A registry with exactly one profile activates that profile automatically.
- A registry with multiple profiles and no current selection remains unset until selected.

The next account switch or shell integration refresh writes version 2 state. Existing profile directories and Keychain credentials are reused without copying or deletion.

## Safety

- Import copies credentials into profile-specific namespaces and leaves system credentials unchanged.
- Removing an active profile requires `--force`.
- Removing the most recently selected profile moves the stable link to another active profile when available.
- Passwords and tokens never appear in metadata or command output.
