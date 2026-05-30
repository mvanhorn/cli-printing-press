# Phase 5.6: Archive Manuscripts — commands

Lazy-loaded archive commands. Run **unconditionally** after promotion (or after lock release for `hold` verdicts). Ordering is load-bearing: archive research/proofs/discovery, strip HAR response bodies before copying, then wipe `$SESSION_DIR` last.

```bash
# Archive under API slug (e.g., steam-web), matching the slug-keyed library layout.
API_SLUG="<api>"
mkdir -p "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID"
cp -r "$RESEARCH_DIR" "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/research" 2>/dev/null || true
cp -f "$API_RUN_DIR/research.json" "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/research.json" 2>/dev/null || true
cp -r "$PROOFS_DIR" "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/proofs" 2>/dev/null || true

# Archive discovery artifacts (browser-sniff captures, URL lists, traffic analysis, browser-sniff report).
# Session state lives outside $DISCOVERY_DIR (see Run Initialization), so the
# archive cannot pick it up. The legacy rm is a no-op safety net for an
# in-flight $DISCOVERY_DIR carried over from a pre-isolation run.
rm -f "$DISCOVERY_DIR/session-state.json" 2>/dev/null || true

# Strip response bodies from HAR before archiving to control size.
if [ -d "$DISCOVERY_DIR" ]; then
  for har in "$DISCOVERY_DIR"/browser-sniff-capture.har "$DISCOVERY_DIR"/browser-sniff-capture.json; do
    if [ -f "$har" ] && command -v jq >/dev/null 2>&1; then
      jq 'del(.log.entries[].response.content.text)' "$har" > "${har}.stripped" 2>/dev/null && mv "${har}.stripped" "$har" || rm -f "${har}.stripped"
    fi
  done
  cp -r "$DISCOVERY_DIR" "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/discovery" 2>/dev/null || true
fi

# Wipe live-auth scratch dir now that the run is archived. The directory lives
# under ${TMPDIR:-/tmp}, so OS-level tmp reaping is the long-tail fallback, but
# we clean explicitly so back-to-back runs do not accumulate session state.
rm -rf "$SESSION_DIR" 2>/dev/null || true
```
