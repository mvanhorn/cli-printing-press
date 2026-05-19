# Secret & PII Scrubbing for Public Artifacts

Read this file during Phase 5 (when writing the retro doc) and during Phase 6
(before zipping artifacts AND before posting issue bodies to GitHub). All scrub
operations work on **temp copies** of the retro doc and issue bodies, never on
the user's original manuscripts or library directories.

## Layer 0: Single-file body scrub (retro doc + issue bodies)

Use this layer when the input is a **single markdown file** (the retro doc at
Phase 5, or each issue/comment body file at Phase 6 Step 3 before `gh issue
create`/`gh issue comment`). It's the public-surface counterpart to Layers 1–4
(which scrub directories of artifacts).

**Why this layer exists.** Layers 1–4 scrub `$STAGING_MANUSCRIPTS` and
`$STAGING_CLI_SOURCE` (the folders zipped and uploaded to catbox). They do
**not** touch the retro doc itself or the issue body text passed to `gh issue
create`. Without Layer 0, a finding's "What we observed" block can paste raw
scanner output, dogfood payloads, or Greptile comments containing real secrets
or PII straight to a public GitHub issue. The retro skill's cardinal rule
("Issue bodies and retro docs are public surfaces") is the human-readable
charter; this layer is the mechanical enforcement.

### scrub_body — reusable shell function

Define this once at the top of the Phase 5 / Phase 6 bash blocks, then call
`scrub_body <input> <output>` to produce a scrubbed copy of a single markdown
file. Returns exit codes that callers branch on.

```bash
# scrub_body <in-file> <out-file>
#
# Scans the input file for vendor-prefix tokens (HARD FAIL) and PII patterns
# (auto-redact). Writes a scrubbed copy to <out-file>. Behavior:
#
#   exit 0  — clean (no findings, or only PII auto-redacted)
#   exit 1  — vendor-prefix token detected; refuses to write output. Caller
#              must hand-redact before proceeding. The offending file path
#              and pattern name go to stderr.
#   exit 2  — write failure or invalid arguments
#
# Hard-fail rationale: vendor-prefix tokens (API keys, OAuth tokens, JWTs)
# are unrecoverable leaks once posted. Auto-redacting them would let an agent
# inadvertently strip a real key the maintainer needs to know about
# (e.g., "the test fixture key on line 14 is real — rotate it"). Forcing the
# agent to redact-and-acknowledge keeps the human (or upstream agent) in the
# loop. PII (emails, phones, real names) is auto-redacted because the
# replacement is lossless for the retro use case.
scrub_body() {
  local in="$1" out="$2"
  if [ -z "$in" ] || [ -z "$out" ] || [ ! -f "$in" ]; then
    echo "scrub_body: usage: scrub_body <in-file> <out-file>" >&2
    return 2
  fi

  # Layer 0a: vendor-prefix HARD-FAIL patterns. Order: most-specific first.
  # Mirrors the patterns in internal/artifacts/secrets.go and Layer 2 below,
  # extended with vendor patterns whose key shape is unambiguous enough to
  # anchor without high false-positive rates (Mailchimp's `-us\d{1,2}`
  # datacenter suffix, Linear's `lin_api_` prefix, Anthropic's `sk-ant-api03-`
  # prefix). Add new patterns here only when the shape is specific enough
  # that the regex cannot match generic placeholder strings.
  local VENDOR_PATTERNS=(
    'stripe-live-key|sk_live_[A-Za-z0-9]{20,}'
    'stripe-test-key|sk_test_[A-Za-z0-9]{20,}'
    'github-pat|ghp_[A-Za-z0-9]{36,}'
    'github-oauth|gho_[A-Za-z0-9]{36,}'
    'github-server|ghs_[A-Za-z0-9]{36,}'
    'slack-bot-token|xoxb-[A-Za-z0-9-]{20,}'
    'slack-user-token|xoxp-[A-Za-z0-9-]{20,}'
    'aws-access-key|\bAKIA[0-9A-Z]{16}\b'
    'openrouter-key|sk-or-v1-[A-Za-z0-9_-]{24,}'
    'anthropic-key|sk-ant-api03-[A-Za-z0-9_-]{40,}'
    'linear-key|\blin_api_[A-Za-z0-9_-]{32,}'
    'mailchimp-key|\b[a-f0-9]{32}-us[0-9]{1,2}\b'
    'jwt-token|\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,}'
    'bearer-with-value|Bearer [A-Za-z0-9._~+/=-]{30,}'
  )

  local hard_fail=0
  for entry in "${VENDOR_PATTERNS[@]}"; do
    IFS='|' read -r name regex <<< "$entry"
    if grep -qE "$regex" "$in" 2>/dev/null; then
      # Surface the finding without quoting the value (the file path + pattern
      # name is enough for the agent to locate and fix; printing the value to
      # stderr defeats the whole point of refusing to write the output).
      lines=$(grep -nE "$regex" "$in" 2>/dev/null | cut -d: -f1 | head -5 | tr '\n' ',' | sed 's/,$//')
      echo "scrub_body: HARD FAIL — $name pattern matched in $in (lines: $lines)" >&2
      hard_fail=1
    fi
  done
  if [ "$hard_fail" -eq 1 ]; then
    echo "scrub_body: refusing to write $out. Hand-redact the matches above with <REDACTED:<vendor>-<kind>:<first4>...<last4>:<len>ch> per references/secret-scrubbing.md Layer 0, then retry." >&2
    return 1
  fi

  # Layer 0b: PII auto-redact patterns. Replaces in the written copy only;
  # input file is untouched. Tags use the same <REDACTED:<kind>> shape as the
  # rest of the scrubbing layers.
  #
  # Allowlist: RFC 2606 reserved example domains (example.com / example.net /
  # example.org / example.invalid / *.test / *.localhost) and NANP fictional
  # phone ranges (555-01XX) are PASS-THROUGH — they exist precisely to be safe
  # in documentation. The email regex anchors the allowlist by excluding the
  # reserved TLDs in the local-part match. The phone regex carves out the
  # 555-01XX range explicitly.
  cp "$in" "$out" 2>/dev/null || { echo "scrub_body: failed to copy $in -> $out" >&2; return 2; }

  # Order matters: specific account-identifier patterns first, then generic
  # email/phone/zip. Otherwise the generic `email` pattern would consume the
  # mailchimp-inbox-id email-shaped string and tag it as `<REDACTED:email>`
  # instead of `<REDACTED:mailchimp-inbox-id>` — both redact the leak, but
  # the specific tag is more useful for diagnostics.

  # Mailchimp inbox-id: us<dc>-<hex>-<hex>@inbound.mailchimp.com is the
  # account-bound inbound mailbox identifier — leaking it exposes the
  # account's identity. Hex segments vary by account; 8+ chars each keeps
  # the false-positive rate near zero (the @inbound.mailchimp.com tail is
  # the strongest discriminator).
  perl -i -pe 's/\bus\d{1,2}-[a-f0-9]{8,}-[a-f0-9]{8,}\@inbound\.mailchimp\.com\b/<REDACTED:mailchimp-inbox-id>/g' "$out" 2>/dev/null

  # Real email (excluding RFC 2606 allowlist). The exclusion uses negative
  # lookbehind via perl since BSD/GNU sed regex flavors differ.
  perl -i -pe 's/\b([A-Za-z0-9._%+-]+)@(?!example\.(?:com|net|org|invalid)\b|[^\s]*\.(?:test|localhost|example)\b)([A-Za-z0-9.-]+\.[A-Za-z]{2,})\b/<REDACTED:email>/g' "$out" 2>/dev/null

  # NANP US phone (excluding 555-01XX fictional range). Matches 10-digit forms
  # with or without dashes/parens/spaces. The 555-01XX exclusion can't use a
  # single negative lookahead anchored at the start because the optional `(`
  # and optional country-code prefix shift the area-code position — when the
  # number is written `(555) 012-3456`, the position before the `(` is not
  # where the `555` lives, so the lookahead never fires. Instead, capture the
  # area code ($1) and exchange ($2) and decide inline via `/e`.
  #
  # Perl gotcha: $& and $1..$N are global variables that get rewritten by
  # every successful regex match — including the inner `$e =~ /^01/` test
  # inside the replacement callback. Snapshot $&, $1, $2 into lexical
  # variables ($w, $a, $e) BEFORE running the inner regex, otherwise the
  # carve-out path returns `"01"` (the inner match) instead of the whole
  # original phone string.
  perl -i -pe 's{(?<![0-9])(?:\+?1[-\s.]?)?\(?([2-9][0-9]{2})\)?[-\s.]?([0-9]{3})[-\s.]?[0-9]{4}(?![0-9])}{my $w=$&; my $a=$1; my $e=$2; ($a eq "555" && $e =~ /^01/) ? $w : "<REDACTED:phone-us>"}ge' "$out" 2>/dev/null

  # ZIP+4 (5 digits, dash, 4 digits). Bare 5-digit ZIPs are too noisy to redact
  # safely (false-positives on order IDs, line counts, etc.); ZIP+4 is the
  # dashed form that's unambiguous.
  perl -i -pe 's/\b\d{5}-\d{4}\b/<REDACTED:zip-plus-4>/g' "$out" 2>/dev/null

  return 0
}
```

### When to call scrub_body

**Phase 5 — after writing the retro doc:**

```bash
# Write retro to $RETRO_PROOF_PATH and $RETRO_SCRATCH_PATH as before, then:
RETRO_PROOF_PATH_SCRUBBED="${RETRO_PROOF_PATH}.scrubbed.md"
if ! scrub_body "$RETRO_PROOF_PATH" "$RETRO_PROOF_PATH_SCRUBBED"; then
  echo "ERROR: retro doc contains an unredacted vendor-prefix secret." >&2
  echo "Open $RETRO_PROOF_PATH, redact the matches reported above per references/secret-scrubbing.md Layer 0, then re-run Phase 5." >&2
  exit 1
fi
mv "$RETRO_PROOF_PATH_SCRUBBED" "$RETRO_PROOF_PATH"
cp "$RETRO_PROOF_PATH" "$RETRO_SCRATCH_PATH"
```

The scrubbed retro doc becomes canonical. The original (with potential PII)
existed in process memory only and is overwritten by the scrubbed version.

**Phase 6 Step 3 — before `gh issue create` and `gh issue comment`:**

```bash
# Build the WU body as before to /tmp/wu1-body.md, then:
if ! scrub_body /tmp/wu1-body.md /tmp/wu1-body-scrubbed.md; then
  echo "ERROR: WU body contains an unredacted vendor-prefix secret. Cannot post." >&2
  # Mark this WU as a failed action in $FAILED_ISSUES so Step 6 surfaces it.
  FAILED_ISSUES+="WU-1 (issue create): scrub_body hard-failed; body file left at /tmp/wu1-body.md for manual redaction.\n"
  continue
fi
gh issue create ... --body-file /tmp/wu1-body-scrubbed.md
```

Same pattern for `gh issue comment`. If `scrub_body` hard-fails, that WU's
filing is skipped and reported in the final summary so the agent (or user)
knows to manually redact and retry.

### Redaction shape reference

When the agent (or a maintainer reviewing the retro doc) needs to hand-redact
a vendor-prefix value, use this format:

```
<REDACTED:<vendor>-<kind>:<first4>...<last4>:<len>ch>
```

Examples:

| Original (anti-pattern) | Redacted (correct) |
|---|---|
| `lin_api_a1b2c3d4e5f6789abcdef0123456789abcdef0123456` | `<REDACTED:linear-api-key:lin_a...0456:48ch>` |
| `22eb323ac258d2e04c3bf9ade77b9c78-us6` | `<REDACTED:mailchimp-api-key:22eb...-us6:36ch>` |
| `ghp_abc123def456ghi789jkl012mno345pqr678stu90` | `<REDACTED:github-pat:ghp_a...tu90:40ch>` |
| `sk_live_51AbCdEf123456...XyZ` | `<REDACTED:stripe-live-key:sk_li...XyZ:N-ch>` |

The first4 + last4 + length fragment preserves enough shape information for a
maintainer to recognize the vendor pattern (and to confirm the same key isn't
recurring across multiple findings) without re-exposing the value. The
`<kind>` tag is enough to drive a follow-up retro finding ("the scanner missed
a `linear-api-key`") without quoting the value.

For PII (`email`, `phone-us`, `zip-plus-4`, `mailchimp-inbox-id`, etc.),
just the `<kind>` tag — no fragment — is the right shape. The maintainer
doesn't need to identify which specific email; they need to know an email
shape was leaked.

### Things to never quote even in redacted form

- **Full URLs containing tokens.** GitHub's secret-scanning "unblock-secret" URLs (e.g., `https://github.com/<org>/<repo>/security/secret-scanning/unblock-secret/<token-id>`) embed a token-id that is itself a credential GitHub uses to authorize the bypass. Even with the token-id redacted, the URL leaks the org/repo/PR triple that the secret-scanning event refers to. Replace the whole URL with `<REDACTED:gh-secret-scanning-url>`.
- **Authorization-header values** in HAR / dogfood-full.json snippets — even when the value looks like a placeholder, treat it as live until proven otherwise. Replace the whole header line with `<REDACTED:authorization-header>`.
- **Account-bound inbound mailboxes / webhook callback URLs / OAuth client-secret pairs.** These are not "secrets" in the API-key sense but identifying the account is a real privacy regression. Tag and redact.

## Layer 1: Exact-value scanning

If the user provided an API key during the generation session, scan for its literal
value and redact it. This has zero false positives.

Use `grep -F` (fixed string) — NOT bare `grep` or `sed` — because API keys often
contain regex metacharacters (`+`, `/`, `.`, `=`).

```bash
# Guard: skip if key is empty or too short (< 16 chars)
if [ -n "$API_KEY_VALUE" ] && [ ${#API_KEY_VALUE} -ge 16 ]; then
  LEAK_FOUND=false
  for dir in "$STAGING_MANUSCRIPTS" "$STAGING_CLI_SOURCE"; do
    if [ -d "$dir" ] && grep -rF "$API_KEY_VALUE" "$dir" 2>/dev/null; then
      LEAK_FOUND=true
    fi
  done
  if [ "$LEAK_FOUND" = true ]; then
    echo "BLOCKING: API key value found in staging artifacts. Auto-redacting."
    REDACT_TO='<REDACTED:session-api-key>'
    for dir in "$STAGING_MANUSCRIPTS" "$STAGING_CLI_SOURCE"; do
      [ -d "$dir" ] || continue
      find "$dir" -type f -print0 | while IFS= read -r -d '' f; do
        if grep -qF "$API_KEY_VALUE" "$f" 2>/dev/null; then
          REDACT_OLD="$API_KEY_VALUE" REDACT_NEW="$REDACT_TO" python3 -c "
import sys, os
old, new, path = os.environ['REDACT_OLD'], os.environ['REDACT_NEW'], sys.argv[1]
with open(path) as f: content = f.read()
with open(path, 'w') as f: f.write(content.replace(old, new))
" "$f"
        fi
      done
    done
    echo "Auto-redacted session API key."
  fi
fi
```

## Layer 2: Pattern-based scanning

Scan for common secret formats regardless of whether a session key was provided.
Each pattern uses `grep -rE` with a concrete regex and a labeled redaction tag.

Run each pattern scan independently. A false positive from one pattern does not
affect other scans.

```bash
# Define patterns: name|regex|redaction-tag
PATTERNS=(
  'stripe-live-key|sk_live_[A-Za-z0-9]{20,}|<REDACTED:stripe-live-key>'
  'stripe-test-key|sk_test_[A-Za-z0-9]{20,}|<REDACTED:stripe-test-key>'
  'github-pat|ghp_[A-Za-z0-9]{36,}|<REDACTED:github-pat>'
  'github-oauth|gho_[A-Za-z0-9]{36,}|<REDACTED:github-oauth>'
  'bearer-token|Bearer [A-Za-z0-9._~+/=-]{20,}|<REDACTED:bearer-token>'
  'jwt-token|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}|<REDACTED:jwt-token>'
)

for entry in "${PATTERNS[@]}"; do
  IFS='|' read -r name regex tag <<< "$entry"
  for dir in "$STAGING_MANUSCRIPTS" "$STAGING_CLI_SOURCE"; do
    [ -d "$dir" ] || continue
    find "$dir" -type f -print0 | while IFS= read -r -d '' f; do
      if grep -qE "$regex" "$f" 2>/dev/null; then
        # Use perl for in-place regex replacement (more reliable than sed across platforms)
        perl -i -pe "s/$regex/$tag/g" "$f" 2>/dev/null
        echo "Redacted $name in $(basename "$f")"
      fi
    done
  done
done
```

### Jurisdiction-specific PII scanning

Live-API responses captured during browser-sniff or live-key dogfood routinely
include identifying data of the data subject and any third parties the API
surfaced. These patterns redact common high-confidence shapes before upload.
They are defense-in-depth, not bulletproof — free-form names, descriptive
fields, and unenumerated jurisdictions still slip through.

The `|` field separator collides with the `(IT|DE|...)` country-code
alternation in the IBAN regex, so PII patterns use `~` as the field separator.

```bash
PII_PATTERNS=(
  'codice-fiscale~\b[A-Z]{6}[0-9]{2}[A-Z][0-9]{2}[A-Z][0-9]{3}[A-Z]\b~<REDACTED:pii-codice-fiscale>'
  'eu-iban~\b(AD|AT|BE|BG|CH|CY|CZ|DE|DK|EE|ES|FI|FR|GB|GI|GR|HR|HU|IE|IS|IT|LI|LT|LU|LV|MC|MT|NL|NO|PL|PT|RO|SE|SI|SK|SM|VA)[0-9]{2}[A-Z0-9]{11,30}\b~<REDACTED:pii-eu-iban>'
  'us-ssn~\b[0-9]{3}-[0-9]{2}-[0-9]{4}\b~<REDACTED:pii-us-ssn>'
)

for entry in "${PII_PATTERNS[@]}"; do
  IFS='~' read -r name regex tag <<< "$entry"
  for dir in "$STAGING_MANUSCRIPTS" "$STAGING_CLI_SOURCE"; do
    [ -d "$dir" ] || continue
    find "$dir" -type f -print0 | while IFS= read -r -d '' f; do
      # Case-insensitive: API JSON routinely lowercases IBANs and other identifiers.
      if grep -qiE "$regex" "$f" 2>/dev/null; then
        perl -i -pe "s/$regex/$tag/gi" "$f" 2>/dev/null
        echo "Redacted $name in $(basename "$f")"
      fi
    done
  done
done
```

Pattern notes:

- **Codice Fiscale** (Italian tax code) is a 16-character `LLLLLLDDLDDLDDDL` shape with no plausible collision against ordinary text.
- **EU IBAN** is anchored to the SEPA country-code prefix list, so the broad `[A-Z0-9]{11,30}` body cannot match a generic phone number, order ID, or vendor SKU.
- **US SSN** uses the `DDD-DD-DDDD` dashed form, which avoids collisions with bare 9-digit runs in other identifiers.

Out of scope (deferred to follow-up work):

- Bare 11-digit Partita IVA / VAT numbers (false-positive rate against order IDs, phone numbers, and timestamps is too high without an allowlist).
- Free-form residential addresses (not regex-matchable with acceptable precision).
- Refusing upload when `discovery/sample-*.json` files are present (a separate gate at the staging-copy step, tracked separately).

### Env var assignment scanning

Separately scan for hardcoded secret assignments in source code:

```bash
# Matches assignments where the variable name ENDS with a secret-like suffix:
#   API_SECRET = "value", AUTH_TOKEN: 'value', API_KEY="value", DB_PASSWORD='value'
# Does NOT match: CACHE_KEY, PRIMARY_KEY, TOKEN_EXPIRY (keyword is not a suffix)
# Only in .go, .env, .yaml, .yml, .json, .toml files
SECRET_ASSIGN_REGEX='[A-Z_]+(SECRET|_TOKEN|_KEY|PASSWORD)\s*[:=]\s*["'"'"'][^"'"'"']{16,}["'"'"']'

for dir in "$STAGING_MANUSCRIPTS" "$STAGING_CLI_SOURCE"; do
  [ -d "$dir" ] || continue
  find "$dir" -type f \( -name "*.go" -o -name "*.env" -o -name "*.yaml" -o -name "*.yml" -o -name "*.json" -o -name "*.toml" \) -print0 | while IFS= read -r -d '' f; do
    if grep -qE "$SECRET_ASSIGN_REGEX" "$f" 2>/dev/null; then
      perl -i -pe 's/[A-Z_]+(SECRET|_TOKEN|_KEY|PASSWORD)\s*[:=]\s*["\x27][^"\x27]{16,}["\x27]/$1=<REDACTED:env-assignment>/g' "$f" 2>/dev/null
      echo "Redacted env assignment in $(basename "$f")"
    fi
  done
done
```

## Layer 3: HAR auth stripping

If the staging manuscripts contain HAR files, strip auth-bearing fields:

```bash
find "$STAGING_MANUSCRIPTS" -name "*.har" -type f -print0 2>/dev/null | while IFS= read -r -d '' har; do
  jq 'del(.log.entries[].response.content.text) |
      (.log.entries[].request.headers) |= [.[] |
        select(.name | test("^(Authorization|Cookie|Set-Cookie|X-API-Key|X-Auth-Token)$"; "i") | not)
      ] |
      (.log.entries[].response.headers) |= [.[] |
        select(.name | test("^(Set-Cookie)$"; "i") | not)
      ] |
      (.log.entries[].request.queryString) |= [.[] |
        if (.name | test("^(key|api_key|apikey|token|secret|access_token|password)$"; "i"))
        then .value = "<REDACTED>"
        else . end
      ] |
      (.log.entries[].request.cookies) |= [] |
      (.log.entries[].response.cookies) |= []
      ' "$har" > "${har}.stripped" 2>/dev/null && mv "${har}.stripped" "$har"
  echo "Stripped auth from $(basename "$har")"
done
```

## Layer 4: Session state cleanup

Remove any session state files that may contain cookies or tokens:

```bash
find "$STAGING_MANUSCRIPTS" -name "session-state.json" -type f -delete 2>/dev/null
```

## Post-scrub verification

After all layers complete, do a final scan for obvious leaks:

```bash
FINAL_CHECK=false
CRED_REGEX='(sk_live_|sk_test_|ghp_|gho_|Bearer [A-Za-z0-9]{20})'
# PII_REGEX must mirror the shapes in PII_PATTERNS above; update both together
# (e.g. when adding Partita IVA with an allowlist) so the verification step
# does not silently stop checking a shape the scrub loop still redacts.
PII_REGEX='(\b[A-Z]{6}[0-9]{2}[A-Z][0-9]{2}[A-Z][0-9]{3}[A-Z]\b|\b(AD|AT|BE|BG|CH|CY|CZ|DE|DK|EE|ES|FI|FR|GB|GI|GR|HR|HU|IE|IS|IT|LI|LT|LU|LV|MC|MT|NL|NO|PL|PT|RO|SE|SI|SK|SM|VA)[0-9]{2}[A-Z0-9]{11,30}\b|\b[0-9]{3}-[0-9]{2}-[0-9]{4}\b)'
for dir in "$STAGING_MANUSCRIPTS" "$STAGING_CLI_SOURCE"; do
  [ -d "$dir" ] || continue
  CRED_MATCHES=$(grep -rEi "$CRED_REGEX" "$dir" 2>/dev/null | grep -v 'REDACTED' | head -5)
  PII_MATCHES=$(grep -rEi "$PII_REGEX" "$dir" 2>/dev/null | grep -v 'REDACTED' | head -5)
  if [ -n "$CRED_MATCHES" ] || [ -n "$PII_MATCHES" ]; then
    [ -n "$CRED_MATCHES" ] && echo "$CRED_MATCHES"
    [ -n "$PII_MATCHES" ] && echo "$PII_MATCHES"
    FINAL_CHECK=true
  fi
done
if [ "$FINAL_CHECK" = true ]; then
  echo "WARNING: Potential secrets or PII still found after scrubbing. Review the matches above."
  echo "Artifacts will NOT be uploaded until this is resolved."
fi
```
