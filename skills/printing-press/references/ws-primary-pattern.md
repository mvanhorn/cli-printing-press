# WebSocket-Primary Sync Pattern

Use this pattern when the target API's primary facts arrive over WebSocket and a REST endpoint supplies descriptive metadata needed for local queries.

## Spec Shape

```yaml
streaming:
  transport: websocket
  url: "wss://api.example.com/v1/ws"
  subscribe_shape: '{"type":"subscribe","channels":["events"]}'
  framing: newline_delimited_json
  metadata:
    endpoint: "/v1/events"
    refresh_cadence: 30s
    statuses: [live, pending]
    primary_key: event_id
```

`framing` may be `single_object_per_frame` or `newline_delimited_json`. When `metadata.statuses` is omitted, the generator polls both `live` and `pending`.

## Emitted Surface

The generator emits:

- `live ws sync` for WebSocket-primary ingest.
- `live rest sync` as the REST polling fallback, reusing the generated `sync` command.
- `internal/wsclient` with dial, subscribe, frame splitting, reconnect-with-backoff, and lifecycle event callbacks.
- Local SQLite tables `<api>_stream_frames`, `<api>_stream_metadata`, and `<api>_rebase_log`.

The WebSocket scaffold stores raw JSON facts and raw metadata rows. Per-API novel code may add typed normalizers beside the generated files, but it should not reimplement the WebSocket lifecycle, metadata-status polling, or rebase-log mechanics.

## Authoring Rules

- Poll every configured metadata status. Default to both `live` and `pending`; polling only `live` misses rows that become live after the frame arrives.
- Use `newline_delimited_json` whenever a vendor can send multiple JSON objects in one WebSocket message. The generated splitter feeds each line separately to `json.Unmarshal`.
- Record reconnect, disconnect, subscribe, metadata, and parse events in `<api>_rebase_log` so operators can diagnose stream gaps.
- Coordinate store writes through the generated live command's shared mutex. Per-API handlers that write additional dimension or fact tables should reuse that pattern.
- Frame normalizers must use `cliutil.ExtractNumber` and `cliutil.ExtractInt` for numeric wire fields that may be JSON strings.
