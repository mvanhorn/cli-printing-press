---
name: printing-press-catalog
description: Browse and install pre-built Go CLIs for popular APIs from the catalog
version: 0.4.0
min-binary-version: "0.2.0"
deprecated: true
allowed-tools:
  - Bash
  - Read
  - Write
  - Glob
  - Grep
  - WebFetch
  - AskUserQuestion
---

# /printing-press-catalog

> **Deprecated:** This skill is superseded by the main `/printing-press` skill, which now checks the built-in catalog automatically. Use `/printing-press <API>` instead. For browsing the catalog, use `printing-press catalog list` in your terminal.

Browse and install pre-built Go CLIs for popular APIs.

## Quick Start

```
/printing-press-catalog
/printing-press-catalog install stripe
/printing-press-catalog search auth
```

## Prerequisites

- Go 1.21+ installed
- `printing-press` binary on PATH (install with `go install github.com/mvanhorn/cli-printing-press/cmd/printing-press@latest`)

## Setup

Before any other commands, run the setup contract to verify the printing-press binary is on PATH and initialize scope variables:

<!-- PRESS_SETUP_CONTRACT_START -->
```bash
# min-binary-version: 0.2.0

# Derive scope first — needed for local build detection
_scope_dir="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
_scope_dir="$(cd "$_scope_dir" && pwd -P)"

# Prefer local build when running from inside the printing-press repo.
if [ -x "$_scope_dir/printing-press" ] && [ -d "$_scope_dir/cmd/printing-press" ]; then
  export PATH="$_scope_dir:$PATH"
  echo "Using local build: $_scope_dir/printing-press"
elif ! command -v printing-press >/dev/null 2>&1; then
  if [ -x "$HOME/go/bin/printing-press" ]; then
    echo "printing-press found at ~/go/bin/printing-press but not on PATH."
    echo "Add GOPATH/bin to your PATH:  export PATH=\"\$HOME/go/bin:\$PATH\""
  else
    echo "printing-press binary not found."
    echo "Install with:  go install github.com/mvanhorn/cli-printing-press/cmd/printing-press@latest"
  fi
  return 1 2>/dev/null || exit 1
fi

PRESS_BASE="$(basename "$_scope_dir" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_-]/-/g; s/^-+//; s/-+$//')"
if [ -z "$PRESS_BASE" ]; then
  PRESS_BASE="workspace"
fi

PRESS_SCOPE="$PRESS_BASE-$(printf '%s' "$_scope_dir" | shasum -a 256 | cut -c1-8)"
PRESS_HOME="$HOME/printing-press"
PRESS_RUNSTATE="$PRESS_HOME/.runstate/$PRESS_SCOPE"
PRESS_LIBRARY="$PRESS_HOME/library"

mkdir -p "$PRESS_RUNSTATE" "$PRESS_LIBRARY"
```
<!-- PRESS_SETUP_CONTRACT_END -->

After running the setup contract, check binary version compatibility. Read the `min-binary-version` field from this skill's YAML frontmatter. Run `printing-press version --json` and parse the version from the output. Compare it to `min-binary-version` using semver rules. If the installed binary is older than the minimum, warn the user: "printing-press binary vX.Y.Z is older than the minimum required vA.B.C. Run `go install github.com/mvanhorn/cli-printing-press/cmd/printing-press@latest` to update." Continue anyway but surface the warning prominently.

Generated CLIs are published to `$PRESS_LIBRARY/`, not to the repo.

## Workflows

### List Catalog (no arguments)

When invoked with no arguments, list all available CLIs grouped by category.

1. Read all YAML files in catalog/ using Glob + Read
2. Parse each file's name, display_name, description, category fields
3. Group by category and display:

```
Available CLIs (12 entries):

Payments:
  stripe - Payment processing and financial infrastructure API
  square - Payment processing and commerce API

Auth:
  stytch - Authentication and user management API

Email:
  sendgrid - Email delivery and marketing API

Communication:
  discord - Chat and community platform API
  twilio - Communication APIs for SMS, voice, and messaging
  front - Customer communication platform API

Developer Tools:
  github - Software development platform API
  digitalocean - Cloud infrastructure and developer platform API

Project Management:
  asana - Work management and project tracking API

CRM:
  hubspot - CRM contacts API

Example:
  petstore - Canonical OpenAPI example

Install any CLI: /printing-press-catalog install <name>
```

### Install (install <name>)

When invoked with `install <name>`:

1. Read catalog/<name>.yaml
2. If file doesn't exist, show error: "No catalog entry for '<name>'. Run /printing-press-catalog to see available CLIs."
3. Extract spec_url from the catalog entry
4. Show preview: "Installing <display_name> CLI from <spec_url>"
5. Download the spec and generate:
   ```bash
   curl -sL -o /tmp/catalog-spec-$$.yaml "<spec_url>"
   OUTPUT_BASE="$PRESS_LIBRARY/<name>-pp-cli"
   OUTPUT_DIR="$OUTPUT_BASE"
   i=2
   while [ -e "$OUTPUT_DIR" ]; do
     OUTPUT_DIR="${OUTPUT_BASE}-$i"
     i=$((i + 1))
   done
   printing-press generate \
     --spec /tmp/catalog-spec-$$.yaml \
     --output "$OUTPUT_DIR" \
     --validate
   ```
7. If all quality gates pass, present the result:
   ```
   Generated <name>-pp-cli with X resources.

   Try it:
     cd "$OUTPUT_DIR"
     go install ./cmd/<name>-pp-cli
     <name>-pp-cli --help
     <name>-pp-cli doctor
   ```
8. If gates fail, show the error and suggest: "Try /printing-press <display_name> API for a custom generation with retry support."

### Search (search <query>)

When invoked with `search <query>`:

1. Read all YAML files in catalog/
2. Search name, display_name, description, and category for the query (case-insensitive)
3. Display matching entries

## Limitations

- Large API specs (Stripe, Discord, GitHub) take 30-60 seconds to generate and compile
- Generated CLIs are truncated to 50 resources / 20 endpoints per resource
- Catalog entries point to external URLs that may change
