# Secret & PII Protection — Implementation Details

Read this file during Phase 5.5 (Archive Manuscripts) and before any publish step.
The cardinal rules in SKILL.md apply at all times. This file has the implementation.

## Exact-value scan before archiving

The skill knows the API key if the user provided one. Before archiving manuscripts,
scan all artifacts for the exact key value. This has zero false positives — it checks
for the specific string, not guessed patterns.

Use `grep -F` (fixed string) and `awk` for replacement — NOT bare `grep`/`sed` —
because API keys often contain regex metacharacters (`+`, `/`, `.`, `=`) that would
cause `grep` to match wrong text and `sed` to corrupt files.

```bash
# Guard: skip if key is empty or too short (< 16 chars). Short strings
# would over-redact legitimate content. Real API keys are 20+ chars.
if [ -n "$API_KEY_VALUE" ] && [ ${#API_KEY_VALUE} -ge 16 ]; then
  LEAK_FOUND=false
  for dir in "$RESEARCH_DIR" "$PROOFS_DIR" "$DISCOVERY_DIR"; do
    if [ -d "$dir" ] && grep -rF "$API_KEY_VALUE" "$dir" 2>/dev/null; then
      LEAK_FOUND=true
    fi
  done
  if [ "$LEAK_FOUND" = true ]; then
    echo "BLOCKING: API key value found in manuscript artifacts. Auto-redacting."
    REDACT_TO="\$${API_KEY_ENV_VAR:-API_KEY}"
    for dir in "$RESEARCH_DIR" "$PROOFS_DIR" "$DISCOVERY_DIR"; do
      [ -d "$dir" ] || continue
      find "$dir" -type f -print0 | while IFS= read -r -d '' f; do
        if grep -qF "$API_KEY_VALUE" "$f" 2>/dev/null; then
          # Use python for truly literal replacement — awk's gsub and perl's
          # s/// both interpret regex metacharacters (+, ., /) in the key,
          # which breaks on JWT tokens and base64-encoded secrets.
          REDACT_OLD="$API_KEY_VALUE" REDACT_NEW="$REDACT_TO" python3 -c "
import sys, os
old, new, path = os.environ['REDACT_OLD'], os.environ['REDACT_NEW'], sys.argv[1]
with open(path) as f: content = f.read()
with open(path, 'w') as f: f.write(content.replace(old, new))
" "$f"
        fi
      done
    done
    echo "Auto-redacted. Verify before proceeding."
  fi
fi
```

## Strip auth from HAR captures before archiving

Credentials can appear in four locations within HAR files:
- **Request headers:** `Authorization: Bearer <token>`, `Cookie: session=...`
- **Response headers:** `Set-Cookie: session=<token>` (from auth flows)
- **Query strings:** `?key=<value>`, `?api_key=<value>`, `?access_token=<value>`
- **Cookies:** session tokens, auth cookies (both request and response)

The archive step must strip all four, plus response bodies (for size):

```bash
jq 'del(.log.entries[].response.content.text) |
    # Remove auth headers from requests
    (.log.entries[].request.headers) |= [.[] |
      select(.name | test("^(Authorization|Cookie|Set-Cookie|X-API-Key|X-Auth-Token)$"; "i") | not)
    ] |
    # Remove Set-Cookie from responses (contains session tokens from auth flows)
    (.log.entries[].response.headers) |= [.[] |
      select(.name | test("^(Set-Cookie)$"; "i") | not)
    ] |
    # Redact auth-like query string params
    (.log.entries[].request.queryString) |= [.[] |
      if (.name | test("^(key|api_key|apikey|token|secret|access_token|password)$"; "i"))
      then .value = "<REDACTED>"
      else . end
    ] |
    # Remove cookies entirely (they often contain session tokens)
    (.log.entries[].request.cookies) |= [] |
    (.log.entries[].response.cookies) |= []
    ' "$har" > "${har}.stripped" 2>/dev/null && mv "${har}.stripped" "$har"
```

## API key handling during the run

When the user provides an API key (Phase 0 API Key Gate or inline):
- Store it only in a shell variable, never in a file
- Pass it to commands via environment variable, not via flags visible in process lists
- In dry-run output, the key may appear in query params — this is expected for
  debugging but must NOT be captured in proof artifacts
- When writing live smoke results to proofs, write the test outcomes (PASS/FAIL)
  but never the request URLs that contain the key in query params

## Workspace & organization PII redaction

Live dogfood testing (Phase 5) naturally surfaces workspace-specific data: organization
names, team names, team member names, and email addresses. This data must NEVER appear
in any public artifact: acceptance reports, shipcheck proofs, manuscripts archived to
the library repo, PR descriptions, or READMEs.

**When reporting live test results, use generic descriptions:**
- "the workspace" not the actual org name
- "5 overloaded members" not their names or emails
- "12 users synced" not "synced matt@company.com, patrick@company.com"
- "team ESP" is OK (team keys are structural, not PII) but "Esper Labs" is not

**Before archiving manuscripts (Phase 5.6):** Scan acceptance reports and shipcheck
proofs for organization names, email addresses, and full names. The exact-value scan
for API keys (above) catches secrets; this step catches PII that the user's live
workspace naturally produces.

**When persisting live-check samples:** `scorecard --live-check` and live-dogfood
sample captures scrub `output_sample` text before it is stored in JSON/proof
artifacts. Treat archive-time and publish-time scans as defense in depth, not as
permission to write raw customer names, emails, addresses, invoice numbers, or
card tails into the run directory.

**Before creating publish PRs:** The publish skill constructs PR descriptions from
manuscripts and test results. Any live test data quoted in the PR body must be
scrubbed of workspace PII. The library repo is public.

## PII pattern scanning

The prose guidance above tells the agent how to compose published artifacts safely.
This section defines a mechanical sweep that runs before publishing to catch the
PII the agent missed. Two prior PII leaks happened despite the prose guidance — a
mechanical defense layer is required.

**Run order:** exact-value scan (above) → HAR auth strip → PII pattern scanning.

**File scope.** Sweep all text-readable files in the staging directory. Detect by
content, not extension — a `.yaml` HAR variant or a `.txt` proof matters as much as
a `.md`. Use this Go-equivalent helper to classify:

```bash
# Treat as text if no NULL byte in the first 8 KiB
isText() { head -c 8192 "$1" | grep -L -q $'\x00' "$1" 2>/dev/null; }
```

(In Bash, `file --mime-type` works on macOS and GNU `file` portably enough.)

### Tier 1: vendor-anchored auto-redact

Auto-redact ONLY when the pattern includes a vendor-specific prefix anchor. These
have near-zero false-positive rate. Auto-redact silently — no user prompt, no
recovery prompt. Pattern set:

```bash
PII_AUTO_REDACT=(
  'openrouter-api-key|sk-or-v1-[A-Za-z0-9_-]{24,}|<REDACTED:openrouter-api-key>'
  'bearer-stripe-live|Bearer sk_live_[A-Za-z0-9]{20,}|Bearer <REDACTED:stripe-live-token>'
  'bearer-stripe-test|Bearer sk_test_[A-Za-z0-9]{20,}|Bearer <REDACTED:stripe-test-token>'
  'bearer-cal-live|Bearer cal_live_[A-Za-z0-9]{20,}|Bearer <REDACTED:cal-live-token>'
  'bearer-cal-test|Bearer cal_test_[A-Za-z0-9]{20,}|Bearer <REDACTED:cal-test-token>'
  'bearer-github-pat|Bearer ghp_[A-Za-z0-9]{36,}|Bearer <REDACTED:github-pat>'
  'bearer-github-oauth|Bearer gho_[A-Za-z0-9]{36,}|Bearer <REDACTED:github-oauth>'
  'bearer-github-server|Bearer ghs_[A-Za-z0-9]{36,}|Bearer <REDACTED:github-server-token>'
  'bearer-github-fine|Bearer github_pat_[A-Za-z0-9_]{60,}|Bearer <REDACTED:github-fine-grained-pat>'
  'slack-user-token|xoxp-[A-Za-z0-9-]{40,}|<REDACTED:slack-user-token>'
  'slack-bot-token|xoxb-[A-Za-z0-9-]{40,}|<REDACTED:slack-bot-token>'
  'slack-app-token|xapp-[A-Za-z0-9-]{32,}|<REDACTED:slack-app-token>'
  'google-api-key|AIza[A-Za-z0-9_-]{20,}|<REDACTED:google-api-key>'
)

for entry in "${PII_AUTO_REDACT[@]}"; do
  IFS='|' read -r name regex tag <<< "$entry"
  for f in $(find "$STAGING_DIR" -type f); do
    isText "$f" || continue
    if grep -qE "$regex" "$f" 2>/dev/null; then
      perl -i -pe "s|$regex|$tag|g" "$f"
      echo "Auto-redacted $name in ${f#$STAGING_DIR/}"
    fi
  done
done
```

Do not add a simple `AKIA[0-9A-Z]{16}` shell auto-redaction rule here. AWS
access keys have the same shape as AWS's canonical documentation placeholder,
so the binary publish scan handles them with a placeholder allowlist instead.

These patterns are vendor-anchored and the prefix character class is restrictive
enough that the false-positive rate is effectively zero. Adding a new vendor
pattern is safe — extend the list when shipping a new printed CLI for a vendor
with a known token format.

### Tier 2: warn-by-default with allowlist

Everything else gets WARN behavior. The user sees the matches, decides whether to
redact each one. This includes generic emails, generic bearer tokens (without a
vendor prefix), and capitalized name patterns. Designed cost asymmetry:
false-positive auto-redaction loses information irreversibly; false-positive
warning costs only a prompt.

```bash
PII_WARN=(
  'email|[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}|email address'
  'bearer-generic|Bearer [A-Za-z0-9._\-+/=]{20,}|generic bearer token (no vendor prefix)'
  'name-pattern|\b[A-Z][a-z]{2,}\s+[A-Z][a-z]{2,}\b|capitalized first+last name'
)
```

**Allowlist (suppress matches against this set):**

Build at scan time from three sources:

1. **Spec-derived terms.** Capitalized two-word phrases extracted from the spec's
   operation summaries, descriptions, tag names, and parameter descriptions. This
   catches "Event Types", "Booking Links", "Webhook Triggers" automatically — they
   look like name patterns but are API vocabulary.

2. **CLI-help-derived terms.** Walk `<cli> --help` recursively and extract
   capitalized two-word phrases from command Short/Long fields. Catches command
   group names that the spec might not mention by exact phrasing.

3. **Universal allowlist.** A small static list of terms that appear across
   APIs and would otherwise produce noise:

   ```
   "Open Source", "Pull Request", "Bearer Token", "API Key", "Access Token",
   "Refresh Token", "GitHub Actions", "Cloud Run", "Service Account",
   "Rate Limit", "Time Zone", "Web Hook", "Personal Access", "Application Token"
   ```

A match is suppressed if it matches any allowlist entry by:
- Exact case-insensitive equality, OR
- Case-insensitive `contains` against the allowlist entry

Email pattern allowlist: any address whose domain matches `example.com`,
`example.org`, `example.net`, `placeholder.com`, or starts with `noreply@`.

### Tier 2 user interaction

When Tier 2 finds matches, present them in a single batched prompt (NOT one
per finding — that creates rubber-stamp fatigue). Per-finding format:

```
PII review (3 findings):

  proofs/acceptance.md:42  email     trevin@trevinchow.com
  proofs/acceptance.md:48  email     henryopenclaw@gmail.com
  proofs/dogfood.md:108   name      Henry Claw

Action:
  [a] Auto-redact all
  [s] Skip all (proceed without redacting)
  [q] Quit and review staging dir at /tmp/.../staging.pre-pii-scrub
```

Pre-scrub copy: before any Tier 1 or Tier 2 mutation runs, copy the staging
directory to `<staging>.pre-pii-scrub/`. This gives the user a recoverable
checkpoint if a redaction was wrong.

### Why warn-by-default

The first design draft auto-redacted emails (permissive pattern) and warned on
names (constrained pattern). Two reviewers independently called this cost-
asymmetry inverted: false-positive auto-redaction permanently corrupts a
manuscript artifact, while false-positive warning costs only a prompt. The
correct policy is the inverse — warn on patterns where any false positive would
lose information; auto-redact only on vendor-prefix-anchored patterns where the
false-positive rate is structurally near-zero.

### Worked examples

**Example 1: vendor token in a proof file.** Input proof file contains:
```
Authorization: Bearer cal_live_7d8911769f5c3d63811e36d95d873a16
```
Tier 1 matches `bearer-cal-live`. Auto-redacts to:
```
Authorization: Bearer <REDACTED:cal-live-token>
```
No user prompt.

**Example 2: real attendee in a dogfood report.** Input proof file contains:
```
Returned 1 real booking (Henry Claw 15-min meeting, attendee henryopenclaw@gmail.com).
```
Tier 2 matches `email` and `name-pattern`. The email's domain (`gmail.com`) is
not in the allowlist; "Henry Claw" is not in spec-derived terms. Both surface in
the batched prompt. User picks `[a]` Auto-redact all → both redacted.

**Example 3: API vocabulary that looks like a name.** Input README contains:
```
The `Event Types` resource lists every bookable meeting type.
```
Tier 2 `name-pattern` matches "Event Types". Spec-derived allowlist includes
"Event Types" (extracted from `tag: "Event Types"` in the spec). Match is
suppressed. No prompt.

**Example 4: clean staged dir.** No matches in either tier. No prompt, publish
proceeds.

## Session state cleanup

Session state files (`session-state.json`) contain live browser cookies and auth
tokens captured during an authenticated browser-sniff run. The containment model
is **by location**, not by archive-time cleanup: `SESSION_STATE_FILE` (set in
SKILL.md's "Run Initialization") points at
`${TMPDIR:-/tmp}/printing-press-$(id -u)/session/$RUN_ID/session-state.json`, outside
`$DISCOVERY_DIR`. The Phase 5.5 archive `cp -r "$DISCOVERY_DIR"` therefore cannot
pick it up, regardless of operator action. After the archive completes, the
Phase 5.5 block also wipes `$SESSION_DIR` so back-to-back runs do not accumulate
session state.

A no-op `rm -f "$DISCOVERY_DIR/session-state.json"` remains in the archive
block as a safety net for in-flight runs carried over from a pre-isolation
skill version; it is not load-bearing for new runs.
