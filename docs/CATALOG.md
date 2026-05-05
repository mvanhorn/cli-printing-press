# Catalog Entry Validation

Catalog entries in `catalog/` are validated by [`internal/catalog/catalog.go`](../internal/catalog/catalog.go). Keep the inline `AGENTS.md` rule in sync with that validator; when the validator's applicability or allowed values change, update the inline trigger sentence in the same PR.

## Why the inline rule is strict

The catalog is embedded into the printing-press binary via `catalog.FS`, so a bad entry is not a local typo; it becomes part of every rebuilt binary. The inline `AGENTS.md` rule keeps the write-time fence close to the edit, while this doc carries the longer rationale and the wrapper-only shape.

`category` and `tier` are deliberately finite enums because they drive catalog browsing, risk expectations, and downstream copy. `other` is the public catch-all. `example` is accepted only as a test-only bucket for fixtures such as `catalog/petstore.yaml`; do not use it for real catalog entries.

## Wrapper-only entries

Wrapper-only entries are the carve-out where `spec_url` and `spec_format` stop being required. The validator treats an entry as wrapper-only when `wrapper_libraries` is non-empty and `spec_url` is empty. In that shape:

- `name`, `display_name`, `description`, `category`, and `tier` are still required.
- `wrapper_libraries[*].name`, `.url`, `.language`, and `.integration_mode` are required.
- Wrapper library URLs must use HTTPS.
- `spec_format` is optional, but if present it must still be one of the allowed formats.

Use the wrapper-only carve-out only when the API is genuinely reached through wrapper libraries rather than a direct spec. If the validator or enum values change, update both this doc and the inline `AGENTS.md` rule together.

## Bearer refresh metadata

Catalog entries for browser-facing APIs with rotating public client bearer tokens may declare `bearer_refresh`. When present, both `bearer_refresh.bundle_url` and `bearer_refresh.pattern` are required, the bundle URL must use HTTPS, and the pattern must compile as a Go regexp.

The generator copies this metadata into the printed CLI so `doctor --refresh-bearer` and the agent-accessible `refresh-bearer` command can refresh the user's stored token from the live source bundle.
