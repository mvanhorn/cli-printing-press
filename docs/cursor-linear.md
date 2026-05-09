# Cursor + Linear (`pp-linear` / `linear-pp-cli`)

After you install the Linear stack with Printing Press, for example:

```bash
npx -y @mvanhorn/printing-press install pp-linear
```

use a **personal Linear API key** so `linear-pp-cli` can call the GraphQL API.

## Where to create the key

In the Linear web app: **Settings → Account → Security & access → Personal API keys** while logged in.

Direct link: [Security & access](https://linear.app/settings/account/security)

This is **not** the same as **Integrations → Connected accounts** (Slack, GitHub, calendar, etc.).

Official overview: [API and Webhooks](https://linear.app/docs/api-and-webhooks).

## Wire the CLI

Either:

```bash
export LINEAR_API_KEY="lin_api_..."
```

or persist (writes `~/.config/linear-pp-cli/config.toml`):

```bash
linear-pp-cli auth set-token "lin_api_..."
```

Then:

```bash
linear-pp-cli doctor
linear-pp-cli sync   # optional; enables offline search and analytics
```

## Deeper Cursor-focused notes

The Linear module in the library maintains a longer guide (MCP vs skill, verification, hygiene):

[CURSOR.md in printing-press-library](https://github.com/mvanhorn/printing-press-library/blob/main/library/project-management/linear/CURSOR.md)
