# Asset Capture Playbook

How to refresh every real screenshot and terminal recording in `assets/`.

The placeholder PNGs and `.cast` stubs that ship in this repo are minimal scaffolds so the project compiles and renders end-to-end before the real assets land. **Replace them before publishing the launch video.** Sub-1MB placeholder PNGs flat-coloured by ffmpeg are the default; once you run the captures below, `git add` will replace them.

---

## Prerequisites (one time)

```bash
brew install asciinema             # for the three terminal recordings
brew install --cask google-chrome-canary
```

You also need the actual Printing Press project running with the three flagship CLIs installed (espn-pp-cli, flight-goat, linear-pp-cli). See the cli-printing-press README quick start.

---

## Claude Desktop screenshots (3 captures)

The video uses three Claude Desktop screenshots to ground the press in real product UI.

### `assets/captures/claude-desktop-empty.png`

A clean Claude Desktop window with the skill list visible in the sidebar. No active conversation. Used as the press-stamp #1 target in the Solution scene.

How to capture:
1. Quit Claude Desktop
2. Launch with a fresh demo profile: `CLAUDE_HOME=~/.claude-launch-video-demo open -a "Claude"`
3. Sidebar should show only the skills you want on camera (`/printing-press`, `/ppl`, `/last30days`)
4. macOS window screenshot: `Cmd+Shift+4` then `Space` then click the Claude window
5. Output goes to Desktop; move into `launch-video/assets/captures/claude-desktop-empty.png`

Required dimensions: 1920x1080 minimum (will be downsampled by HtmlInCanvas)

### `assets/captures/claude-desktop-printing-press-running.png`

Claude Desktop mid-run on `/printing-press <api>`, showing the press in Phase 1 (Research) or Phase 1.5 (Absorb). Used as the **Problem scene background** that gets blurred behind the chaos overlay.

How to capture:
1. In a fresh Claude Desktop session, run `/printing-press notion`
2. Wait until Phase 1 or 1.5 streaming is on screen
3. Capture: `Cmd+Shift+4 + Space + click`
4. Move to `launch-video/assets/captures/claude-desktop-printing-press-running.png`

### `assets/captures/claude-desktop-cli-output.png`

Claude Desktop showing the press output once a CLI is generated (Phase 4 shipcheck or final summary). Used as a fallback shot if the press-stamp #2 cuts to it.

---

## printingpress.dev screenshots (3 captures)

### `assets/captures/printingpress-dev-hero.png`

The landing page hero of `printingpress.dev`. Browser viewport at 1920x1080, no scroll.

How to capture (using Claude in Chrome MCP):
1. Open Chrome, navigate to `https://printingpress.dev/`
2. Wait for hero animations to settle (3 seconds)
3. macOS: `Cmd+Shift+4` to grab the hero region
4. Or use the `mcp__claude-in-chrome__gif_creator` to record an idle 2s GIF and extract the first frame

### `assets/captures/printingpress-dev-catalog.png`

The library catalog grid (24 CLIs, organised by category). Scroll to the catalog section before capturing.

### `assets/captures/printingpress-dev-espn-detail.png`

The ESPN-pp-cli detail page on the catalog (or the homepage callout for ESPN). Reinforces the Proof-scene #1 stat.

---

## Terminal recordings (3 asciinema casts)

Each `.cast` file is a JSON-encoded asciinema session that the `TerminalEmu` component replays at video frame rate. The current files are hand-authored stubs; replace with real recordings of the actual CLIs.

### `assets/terminal/espn-command.cast`

```bash
cd ~/printing-press/library/espn-pp-cli
asciinema rec --overwrite \
  --command "espn-pp-cli live --sport nba" \
  ~/cli-printing-press/launch-video/assets/terminal/espn-command.cast
# Run the command; let output finish; press Ctrl-D
```

Make sure the live ESPN response shows a current game in progress. If recording outside NBA season, use `--sport mlb` or `--sport nfl` instead.

### `assets/terminal/flightgoat-command.cast`

```bash
asciinema rec --overwrite \
  --command "flight-goat sea-jfk --pax 4 --depart 2026-12-24 --return 2027-01-01" \
  ~/cli-printing-press/launch-video/assets/terminal/flightgoat-command.cast
```

If the dates are too soon (no fares), shift forward a week. The output should show 4-5 nonstop options with mixed `[kayak]` / `[google]` source tags.

### `assets/terminal/linear-command.cast`

```bash
cd ~/printing-press/library/linear-pp-cli
linear-pp-cli sync           # ensure local mirror is fresh
asciinema rec --overwrite \
  --command "linear-pp-cli blocked --since 7d" \
  ~/cli-printing-press/launch-video/assets/terminal/linear-command.cast
```

Output should show 5+ blocked issues. If your Linear workspace is empty, fall back to a demo workspace or scrub the cast file's IDs to anonymised placeholders before committing.

---

## Privacy review (mandatory before commit)

Every asset goes through this gate before `git add`:

- [ ] No real customer names, account IDs, or email addresses
- [ ] No active session tokens visible (Claude Desktop will show partial keys in the URL bar of skill UIs - crop or blur)
- [ ] No private channel names or DM threads
- [ ] No proprietary internal data (Linear titles that mention unreleased features)

Use a synthetic demo workspace for the recordings if your real workspace cannot pass these checks.

---

## Asset retention policy

Real captures live in `assets/captures/` (PNG, max ~5MB each) and `assets/terminal/` (JSON `.cast`, < 50KB each). They check directly into git for now. If asset weight exceeds 25MB, configure git LFS for the `assets/` directory and update this playbook.
