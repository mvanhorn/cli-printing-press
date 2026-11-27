# App-Sniff Implementation (Desktop & Mobile Binary Discovery)

> **When to read:** Referenced by [Phase 1.7](../phases/06-browser-sniff-gate.md),
> [Phase 1.8](../phases/07-crowd-sniff-gate.md), and `/printing-press-polish`.
> Read this when no public OpenAPI spec exists and the target ships a desktop
> app (macOS `.app` / Electron `app.asar` / Chromium Service Worker
> `CacheStorage`) or mobile app (Android `.apk`/`.xapk`/`.aab`, `--apkeep`,
> `--adb`, iOS `.ipa`, or Simulator `.app`).

## Safety Boundaries

- **Read-only static analysis.** `app-sniff` never executes target app code,
  never bypasses FairPlay DRM or TLS certificate pinning, and never reads
  browser or app credential stores (`Cookies`, `Local Storage`,
  `Session Storage`, `IndexedDB`, `Keychain`, `.sqlite`, `.ldb`, `.env`).
- **No secret leakage.** Credential values, JWTs, bearer tokens, and raw cookie
  values are redacted before writing specs, `traffic-analysis.json`, or
  manifests. Only auth scheme hints and header/cookie names are recorded.

## Resolution Order

Check sources in this order and stop once a usable spec is produced:

1. **Installed desktop app (macOS Electron / web wrapper)** — check
   `/Applications/<App>.app`. `app-sniff --app` reads `Contents/Info.plist`,
   extracts `Contents/Resources/*.asar` / unpacked JS, and automatically scans
   the matching `Service Worker/CacheStorage` for thin-shell apps.
2. **Android package** — if the user supplied an `.apk`/`.xapk`/`.aab`, pass
   `--android <path>`. If `apkeep` is installed and the reverse-DNS package ID
   is known, pass `--apkeep <package-id>`. If an emulator or device is attached
   over ADB, pass `--adb <package-id>`. Pure-Go DEX parsing recovers exact
   Retrofit `@GET`/`@POST`/`@PUT`/`@PATCH`/`@DELETE`/`@Query`/`@Path`
   annotations, `resources.arsc` API base URLs, Hermes HBC strings, Flutter AOT
   strings, and `network_security_config.xml` TLS pinning.
3. **iOS bundle** — if an unencrypted `.ipa` or Simulator `.app` is available,
   pass `--ios <path>` or `--simulator <bundle-id>`. FairPlay-encrypted App
   Store Mach-O binaries are detected via `LC_ENCRYPTION_INFO_64` and skipped
   honestly while unencrypted assets (`Info.plist`, `main.jsbundle`, JSON) are
   still inspected.

## Commands

Ensure the discovery directory exists and verify the binary supports `app-sniff`:

```bash
mkdir -p "$DISCOVERY_DIR"
"$PRINTING_PRESS_BIN" app-sniff --help >/dev/null 2>&1
```

### Desktop app

```bash
"$PRINTING_PRESS_BIN" app-sniff \
  --app "/Applications/<App>.app" \
  --name "<api>" \
  --output "$RESEARCH_DIR/<api>-app-sniff-spec.yaml" \
  --analysis-output "$DISCOVERY_DIR/traffic-analysis.json" \
  --manifest-output "$DISCOVERY_DIR/app-sniff-manifest.json" \
  --json > "$DISCOVERY_DIR/app-sniff-summary.json"
```

### Android package (local `.xapk`/`.apk` or `--apkeep` / `--adb`)

```bash
"$PRINTING_PRESS_BIN" app-sniff \
  --apkeep "<com.vendor.app>" \
  --name "<api>" \
  --output "$RESEARCH_DIR/<api>-app-sniff-spec.yaml" \
  --analysis-output "$DISCOVERY_DIR/traffic-analysis.json" \
  --manifest-output "$DISCOVERY_DIR/app-sniff-manifest.json" \
  --json > "$DISCOVERY_DIR/app-sniff-summary.json"
```

Pass `--include-grpc` only when the user explicitly wants Protobuf/gRPC service
paths in the spec; by default, gRPC routes are preserved in
`app-sniff-manifest.json` and excluded from REST spec generation.

### Optional HAR reconciliation

When a HAR or enriched capture is also available, pass `--har <capture.har>` to
reconcile static routes against observed traffic (`confirmed`, `observed-only`,
`static-only`) and promote observed HTTP methods, status codes, and auth header
names:

```bash
"$PRINTING_PRESS_BIN" app-sniff \
  --android "<path/to/app.xapk>" \
  --har "$DISCOVERY_DIR/capture.har" \
  --name "<api>" \
  --output "$RESEARCH_DIR/<api>-app-sniff-spec.yaml" \
  --analysis-output "$DISCOVERY_DIR/traffic-analysis.json"
```

## Feeding Into Phase 2 (`generate`)

- **Primary discovery:** pass `--spec "$RESEARCH_DIR/<api>-app-sniff-spec.yaml" --traffic-analysis "$DISCOVERY_DIR/traffic-analysis.json" --spec-source sniffed` to `generate`.
- **Enrichment mode:** pass `--spec <existing-spec> --spec "$RESEARCH_DIR/<api>-app-sniff-spec.yaml" --name "<api>"` to merge newly discovered routes with an existing spec.

## Auditing an Existing Printed CLI (`/printing-press-polish` / `/printing-press-amend`)

When polishing or amending a printed CLI for a service that ships a local
desktop or mobile app, run `--audit-cli` to surface version drift, unwired
backend shards, missing endpoints, and MCP `codeOrchEndpoints` parity gaps:

```bash
"$PRINTING_PRESS_BIN" app-sniff \
  --app "/Applications/<App>.app" \
  --audit-cli "$CLI_DIR" \
  --json
```
