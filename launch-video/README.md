# The Printing Press - Launch Video

A 45-second launch video for [printingpress.dev](https://printingpress.dev/), built in Remotion v4.0.455+ using the HTML-in-canvas primitive shipped on 2026-05-04.

## What this is

Five scenes, one composition, 1350 frames at 30fps:

| Scene | Frames | Duration | Beat |
|-------|--------|----------|------|
| Hook | 0-90 | 0-3s | Six tools, six syntaxes, one agent (HTML-in-canvas glitch) |
| Problem | 90-300 | 3-10s | Agents waste 60% of their tokens hunting |
| Solution | 300-750 | 10-25s | One command. The press prints. (HTML-in-canvas press-stamp) |
| Proof | 750-1140 | 25-38s | ESPN, flight-goat, linear (1 call / 2 sources / 50ms) |
| CTA | 1140-1350 | 38-45s | Wordmark + URL + install command (HTML-in-canvas ink-assemble) |

Final outputs:
- `out/hero-45s.mp4` - 1920x1080, 45s
- `out/hero-vertical-45s.mp4` - 1080x1920, 45s (X / TikTok)
- `out/hero-square-45s.mp4` - 1080x1080, 45s (LinkedIn)
- `out/cutdown-15s.mp4` - 1920x1080, frames 270-720 of the hero (X autoplay)

## Setup

### 1. Install dependencies

```bash
cd launch-video
npm install
```

### 2. Install Remotion Agent Skills (mandatory for Claude Code authoring)

```bash
npm run skills:install
```

This installs `remotion-dev/skills` (the 28 modular rule files Claude Code loads on every prompt) plus the dedicated `html-in-canvas` rule. Claude Code will auto-discover them on next session start.

If you forget this step, Claude Code will still write Remotion code - but it will not know the html-in-canvas lifecycle (`onInit` / `onPaint`), the `--gl=angle` requirement, or the no-nesting rule. Output will be subtly wrong.

### 3. Install Chrome Canary 149+ (one-time, host machine)

HTML-in-canvas is unstable. As of 2026-05-04 it ships in Chrome Canary v149+ behind a flag.

```bash
brew install --cask google-chrome-canary
```

Open Chrome Canary, navigate to `chrome://flags/#canvas-draw-element`, and toggle to **Enabled**. Restart Chrome Canary.

Remotion v4.0.455+ bundles a compiled Canary build for headless rendering with the flag pre-enabled, so Step 3 is only required for **previewing** in `npm run preview`. Headless rendering with `npm run render` works without a host Chrome Canary.

## Render

```bash
# All four outputs (hero + vertical + square + cutdown)
npm run render:all

# Or one at a time
npm run render
npm run render:vertical
npm run render:square
npm run render:cutdown
```

Headless render uses `--gl=angle` (set in `remotion.config.ts`), which routes through the bundled Canary build with the canvas-draw-element flag pre-enabled.

## Preview

```bash
npm run preview
```

Opens the Remotion Studio at http://localhost:3000. Pick a composition (Hero / HeroVertical / HeroSquare) and scrub the timeline. Studio uses your host Chrome Canary, so Step 3 above is required.

## Asset capture

Real screenshots and terminal recordings are version-controlled in `assets/`. To recapture:

See `scripts/capture.md` for the full playbook (Claude Desktop screenshots, printingpress.dev recordings, asciinema sessions for the three flagship CLIs).

## Audio

Music track and SFX live in `assets/audio/`. License attribution in `assets/audio/LICENSE.md`.

If you do not have a music track yet, the project still renders silently - the `<Audio>` component fails gracefully on missing source. Final output is muted but visually correct.

## Ship checklist

Before tweeting:

- [ ] All assets check in clean from a fresh clone (`git clean -xdf && npm install && npm run render:all`)
- [ ] All 4 MP4s render without HTML-in-canvas warnings
- [ ] Hero MP4 plays in QuickTime, X (muted + unmuted), Chrome `<video>`, Safari iOS
- [ ] Vertical MP4 plays correctly in X mobile feed
- [ ] Cutdown MP4 stands alone narratively (cold viewer can recall command + URL)
- [ ] License attribution for music in `assets/audio/LICENSE.md` AND in this README's Credits section
- [ ] No PII in any captured screenshot (tokens, customer names, channel names)
- [ ] Final frame (1350) is screenshot-worthy as the X share preview
- [ ] Re-rendering on a clean checkout produces the same MP4 (deterministic render check)

## Project layout

```
launch-video/
├── src/
│   ├── Root.tsx                    # Composition registration
│   ├── compositions/Hero.tsx       # The single composition; 5 scenes via <Series>
│   ├── scenes/                     # Hook / Problem / Solution / Proof / CTA
│   ├── components/                 # PressStamp / GlitchOverlay / WordmarkInkAssemble / TerminalEmu / ClaudeDesktopFrame / BrowserChromeFrame
│   ├── shaders/                    # press-stamp.glsl / glitch.glsl / ink-assemble.glsl
│   ├── timing/schedule.ts          # Single source of truth for frame boundaries
│   └── copy/script.ts              # All on-screen text
├── assets/
│   ├── captures/                   # Real PNG screenshots (Claude Desktop + printingpress.dev)
│   ├── terminal/                   # asciinema .cast recordings (ESPN / flight-goat / linear)
│   ├── audio/                      # Music + SFX
│   └── fonts/                      # Geist Mono + Inter
├── scripts/
│   ├── capture.md                  # Asset recapture playbook
│   └── render-all.sh               # Renders all 4 outputs
└── out/                            # Rendered MP4s (gitignored)
```

## Source plan

Built from `docs/plans/2026-05-04-001-feat-printing-press-launch-video-plan.md` in the cli-printing-press repo. Read the plan for the full requirements trace, scope boundaries, and implementation-unit breakdown.

## Credits

- Music track: TBD - licensed via Musicbed or Artlist (see `assets/audio/LICENSE.md`)
- Fonts: [Geist Mono](https://vercel.com/font), [Inter](https://rsms.me/inter/)
- Built with [Remotion](https://remotion.dev) v4.0.455+ using the HTML-in-canvas primitive shipped 2026-05-04 by [@Remotion](https://x.com/Remotion)
- Author: [@mvanhorn](https://x.com/mvanhorn)
