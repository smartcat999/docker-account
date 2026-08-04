# Product

## Register

product

## Users

Developers and operators who use Docker Hub and private registries from the terminal, especially people who must switch identities for the same registry without repeatedly logging in.

## Product Purpose

Manage switchable Docker registry identities while preserving the Docker contexts, builders, plugins, and unrelated registry logins that already work on the machine. Success means the active identity is unambiguous, switching is fast, and existing Docker data remains safe.

## Brand Personality

Quiet, familiar, trustworthy. The interface should feel like a native terminal utility: concise copy, predictable keys, restrained color, and clear outcomes.

## Anti-references

Avoid dashboard-like terminal screens, bright blue decoration, repeated status information, crowded columns, color-only meaning, and prompts that hide what data will change.

## Design Principles

- Keep one active identity per registry so unrelated registry logins coexist.
- Preserve system credentials and shared Docker runtime data by default.
- Make destructive scope and credential ownership explicit before acting.
- Keep the primary screen focused on the current identity and intended action.
- Never print passwords, tokens, or encoded credentials.

## Accessibility & Inclusion

All state must be understandable without color. Support keyboard-only interaction, narrow terminals, non-interactive fallback output, terminal color preferences, and clear text labels alongside symbols.
