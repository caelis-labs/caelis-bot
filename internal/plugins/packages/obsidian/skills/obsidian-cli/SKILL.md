---
name: obsidian-cli
description: Use Obsidian's official desktop CLI for a user-requested note, vault search, task, property, or link operation.
---

# Obsidian CLI

Use this Skill when the user asks you to work with an Obsidian vault. The Skill supplies guidance only. Installing it does not install Obsidian, enable its CLI, open a vault, or grant access. Use the current Runtime's normal command tool and approval policy.

Obsidian must be running with **Command line interface** enabled in Settings > General. First check `obsidian version` and the relevant command's `obsidian help <command>` if the CLI or syntax is uncertain. If the CLI is unavailable, explain the setting to the user. Do not replace it with raw vault file editing or a third-party service.

Choose a vault deliberately. Use the exact name supplied by the user as the **first argument**, for example `obsidian vault="My Vault" files`. Check `obsidian vault="My Vault" vault info=name` and require the exact name in output before reading or changing notes. The CLI can print an error while returning exit code zero; inspect its output. If the user has not identified a vault, ask which vault to use. Never rely on the active vault or current directory as the target.

For a note, prefer `path=<vault-relative path>` over the active file or ambiguous `file=<name>`. Useful read commands include `files`, `search query="..."`, and `read path="..."`; use bounded `limit=` when searching. Before a change, inspect the exact target and follow the user's requested scope. Use `silent` for `create` so a background note operation does not open a window. Do not add `overwrite` without explicit authority. For a missing response after a write, inspect the original target before considering another attempt; do not blindly repeat it.

The CLI also exposes developer commands, including `eval`. Do not use those as a shortcut for note access. For additional supported operations, consult the installed `obsidian help` and the [official CLI reference](https://obsidian.md/help/cli).
