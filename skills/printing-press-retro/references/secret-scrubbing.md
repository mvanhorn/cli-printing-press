# Secret & PII Scrubbing for Public Artifacts

Read this file during Phase 6 before zipping and uploading artifacts to catbox.moe.
All scrub operations work on the **temp staging copy**, never on the user's original
manuscripts or library directories.

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
      if grep -qE "$regex" "$f" 2>/dev/null; then
        perl -i -pe "s/$regex/$tag/g" "$f" 2>/dev/null
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
PII_REGEX='(\b[A-Z]{6}[0-9]{2}[A-Z][0-9]{2}[A-Z][0-9]{3}[A-Z]\b|\b(AD|AT|BE|BG|CH|CY|CZ|DE|DK|EE|ES|FI|FR|GB|GI|GR|HR|HU|IE|IS|IT|LI|LT|LU|LV|MC|MT|NL|NO|PL|PT|RO|SE|SI|SK|SM|VA)[0-9]{2}[A-Z0-9]{11,30}\b|\b[0-9]{3}-[0-9]{2}-[0-9]{4}\b)'
for dir in "$STAGING_MANUSCRIPTS" "$STAGING_CLI_SOURCE"; do
  [ -d "$dir" ] || continue
  CRED_MATCHES=$(grep -rEi "$CRED_REGEX" "$dir" 2>/dev/null | grep -v 'REDACTED' | head -5)
  PII_MATCHES=$(grep -rE "$PII_REGEX" "$dir" 2>/dev/null | grep -v 'REDACTED' | head -5)
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
