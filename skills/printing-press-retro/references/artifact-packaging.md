# Artifact Packaging and Upload

Read this file during Phase 6, Step 1 (packaging) and Step 3 (uploading).

Steps 1-4 run during Phase 6 Step 1 to prepare artifacts locally.
Step 5 runs during Phase 6 Step 3 **after the user confirms** they want to submit.
Step 6 (failure handling) runs if Step 5 encounters errors.
Step 7 (cleanup) runs at the very end of Phase 6, after everything else is done.

**Cardinal rule:** Never modify the user's source directories. All operations work on
temporary copies. If anything fails, the user's manuscripts and library are untouched.

## Step 1: Create staging directory

```bash
STAGING_DIR=$(mktemp -d)
STAGING_MANUSCRIPTS="$STAGING_DIR/manuscripts"
STAGING_CLI_SOURCE="$STAGING_DIR/cli-source"
echo "Staging artifacts in $STAGING_DIR"
```

## Step 2: Copy artifacts to staging

Copy the manuscript run directory:

```bash
mkdir -p "$STAGING_MANUSCRIPTS"
cp -r "$RUN_DIR/." "$STAGING_MANUSCRIPTS/"
```

Copy the CLI source if available. Skip if `CLI_DIR` is empty (manuscripts-only mode):

```bash
if [ -n "$CLI_DIR" ] && [ -d "$CLI_DIR" ]; then
  mkdir -p "$STAGING_CLI_SOURCE"
  rsync -a \
    --exclude="$CLI_NAME" \
    --exclude="vendor/" \
    --exclude="go.sum" \
    --exclude=".git/" \
    --exclude="*.test" \
    --exclude="*.exe" \
    "$CLI_DIR/" "$STAGING_CLI_SOURCE/"

  # If rsync is not available (rare but possible), fall back to:
  # cp -r "$CLI_DIR/." "$STAGING_CLI_SOURCE/"
  # rm -f "$STAGING_CLI_SOURCE/$CLI_NAME"
  # rm -rf "$STAGING_CLI_SOURCE/vendor" "$STAGING_CLI_SOURCE/.git"
  # rm -f "$STAGING_CLI_SOURCE/go.sum"
  # find "$STAGING_CLI_SOURCE" -name "*.test" -o -name "*.exe" -delete 2>/dev/null
else
  echo "No CLI source directory available. Packaging manuscripts only."
  STAGING_CLI_SOURCE=""
fi
```

## Step 3: Scrub secrets

Read and apply [references/secret-scrubbing.md](references/secret-scrubbing.md) on
the staging copies. The scrub file expects `$STAGING_MANUSCRIPTS` and
`$STAGING_CLI_SOURCE` to be set.

If the post-scrub verification reports unresolved secrets, **do not proceed with upload**.
Save the zips locally and tell the user to review manually.

## Step 4: Zip artifacts

```bash
MANUSCRIPTS_ZIP="$STAGING_DIR/$API_SLUG-manuscripts.zip"
CLI_SOURCE_ZIP=""

(cd "$STAGING_MANUSCRIPTS" && zip -r "$MANUSCRIPTS_ZIP" . -x "*.DS_Store") 2>/dev/null
echo "Manuscripts zip: $(du -h "$MANUSCRIPTS_ZIP" | cut -f1)"

if [ -n "$STAGING_CLI_SOURCE" ] && [ -d "$STAGING_CLI_SOURCE" ]; then
  CLI_SOURCE_ZIP="$STAGING_DIR/$API_SLUG-cli-source.zip"
  (cd "$STAGING_CLI_SOURCE" && zip -r "$CLI_SOURCE_ZIP" . -x "*.DS_Store") 2>/dev/null
  echo "CLI source zip: $(du -h "$CLI_SOURCE_ZIP" | cut -f1)"
fi
```

## Step 5: Upload to catbox.moe

Upload each zip and capture the returned URL:

```bash
MANUSCRIPTS_URL=""
CLI_SOURCE_URL=""
UPLOAD_FAILED=false

# Upload manuscripts
RESPONSE=$(curl -s -F "reqtype=fileupload" -F "fileToUpload=@$MANUSCRIPTS_ZIP" https://catbox.moe/user/api.php 2>/dev/null)
if echo "$RESPONSE" | grep -q "^https://"; then
  MANUSCRIPTS_URL="$RESPONSE"
  echo "Manuscripts uploaded: $MANUSCRIPTS_URL"
else
  echo "WARNING: Failed to upload manuscripts to catbox.moe. Response: $RESPONSE"
  UPLOAD_FAILED=true
fi

# Upload CLI source (only if it was packaged)
if [ -n "$CLI_SOURCE_ZIP" ] && [ -f "$CLI_SOURCE_ZIP" ]; then
  RESPONSE=$(curl -s -F "reqtype=fileupload" -F "fileToUpload=@$CLI_SOURCE_ZIP" https://catbox.moe/user/api.php 2>/dev/null)
  if echo "$RESPONSE" | grep -q "^https://"; then
    CLI_SOURCE_URL="$RESPONSE"
    echo "CLI source uploaded: $CLI_SOURCE_URL"
  else
    echo "WARNING: Failed to upload CLI source to catbox.moe. Response: $RESPONSE"
    UPLOAD_FAILED=true
  fi
else
  echo "No CLI source to upload (manuscripts-only mode)."
fi
```

## Step 6: Handle upload failure (R15)

If either upload failed:

```bash
if [ "$UPLOAD_FAILED" = true ]; then
  # Preserve local zips for manual attachment
  LOCAL_ZIP_DIR="$RUN_DIR/proofs"
  cp "$MANUSCRIPTS_ZIP" "$LOCAL_ZIP_DIR/" 2>/dev/null
  cp "$CLI_SOURCE_ZIP" "$LOCAL_ZIP_DIR/" 2>/dev/null
  echo ""
  echo "Some artifacts could not be uploaded to catbox.moe."
  echo "Local copies saved to: $LOCAL_ZIP_DIR/"
  echo "You can upload them manually or attach them to the GitHub issue."
fi
```

## Step 7: Cleanup

**Called by the SKILL.md at the end of Phase 6**, after issue creation, local saves,
and presenting results are all complete. Do not call this immediately after zipping —
the staging folder must stay alive for user review and upload.

```bash
rm -rf "$STAGING_DIR"
```

If cleanup fails (permissions, etc.), it's a temp directory — the OS will clean it up
eventually. Do not let cleanup failure block the rest of the workflow.

## Variables expected by this reference

| Variable | Set by | Contains |
|----------|--------|----------|
| `$RUN_DIR` | SKILL.md Phase 1 | Full path to the manuscript run directory |
| `$CLI_DIR` | SKILL.md Phase 1 | Full path to the CLI library directory |
| `$CLI_NAME` | SKILL.md guard rails | Binary name (e.g., `cal-com-pp-cli`) |
| `$API_SLUG` | SKILL.md guard rails | API slug (e.g., `cal-com`) |
| `$API_KEY_VALUE` | User session (optional) | The API key if provided during generation |

## Variables produced by this reference

| Variable | Contains |
|----------|----------|
| `$MANUSCRIPTS_URL` | catbox URL for manuscripts zip, or empty if upload failed |
| `$CLI_SOURCE_URL` | catbox URL for CLI source zip, or empty if upload failed |
| `$UPLOAD_FAILED` | `true` if any upload failed, `false` otherwise |
| `$MANUSCRIPTS_ZIP` | Local path to manuscripts zip (in staging, deleted after cleanup) |
| `$CLI_SOURCE_ZIP` | Local path to CLI source zip (in staging, deleted after cleanup) |
