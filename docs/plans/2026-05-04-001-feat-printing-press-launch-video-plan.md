---
title: "feat: Killer 45-second launch video for The Printing Press (Remotion + HTML-in-canvas)"
type: feat
status: completed
date: 2026-05-04
---

# feat: Killer 45-second launch video for The Printing Press (Remotion + HTML-in-canvas)

## Summary

Plan a 45-second hero launch video for [printingpress.dev](https://printingpress.dev/) built in Remotion v4.0.455+, using the brand-new HTML-in-canvas primitive (shipped May 4 by [@Remotion](https://x.com/Remotion)) to render real Claude Desktop and printingpress.dev screenshots as live DOM nodes, then apply press-stamp, ink-bleed, and glitch transitions on top. The video follows the proven Hook-Problem-Solution-Proof-CTA arc compressed to 45s, opens on the agent-tab-thrash pain, lands on a single `/printing-press <api>` command, and closes on the wordmark assembling from "ink." Built as a new `launch-video/` Remotion project inside the cli-printing-press repo.

---

## Problem Frame

The Printing Press has a complex pitch: it absorbs every competing tool, generates a Go CLI plus MCP server plus Claude Code skill, runs a 9-phase pipeline, and ships a 100-point scorecard. The README is excellent prose; it is also 7,000 words. A founder, dev tool buyer, or AI-native engineer scrolling X for 8 seconds will not read 7,000 words. The launch needs a video that lands the entire promise in 45 seconds with one rewatchable moment, one "wait, what?" beat, and one clear command they can copy-paste. Without it, the launch tweet is just another text post.

Constraint: the video has to feel like it could only have been made with The Printing Press's tooling. A generic After Effects template defeats the message. The medium is the message - if Claude Code + Remotion can produce this video, then Claude Code + Printing Press can produce that CLI. HTML-in-canvas is the unfair advantage because it is the freshest primitive in the ecosystem (shipped today) and lets us render real product UI as living, post-processable footage.

---

## Requirements

- R1. Final asset is one 45-second 1920x1080 H.264 MP4 at 30fps, plus a 1080x1920 vertical cut for X/TikTok and a 1080x1080 square cut for LinkedIn
- R2. Hits the Hook (0-3s) / Problem (3-10s) / Solution (10-25s) / Proof (25-38s) / CTA (38-45s) timing windows
- R3. Uses Remotion `<HtmlInCanvas>` for at least three signature shots: opening tab-thrash glitch, the press-stamp transition, and the wordmark ink-assemble
- R4. Embeds at least three real Claude Desktop screenshots and at least two real printingpress.dev screenshots as in-canvas DOM (not flat PNGs)
- R5. Ends on a single, copyable command line and the URL `printingpress.dev`
- R6. Renders deterministically: same prompt + same git SHA produces byte-identical output (Remotion's promise)
- R7. Produces a tweetable 15-second cutdown built from the same composition (subset of frames, no re-edit)

**Origin actors:** Founder/buyer scrolling X (primary), AI-native dev evaluating tooling (secondary), Existing Claude Code user who would install on first watch (tertiary)
**Origin flows:** F1 Watch full 45s on landing page hero, F2 Watch 6-8s autoplay loop in X feed, F3 Click through to `printingpress.dev` from end-card CTA
**Origin acceptance examples:** AE1 (covers R1, R2, R5) - viewer who watches the 15s cutdown can recall the command and the URL unprompted; AE2 (covers R3, R4) - a Remotion-savvy viewer recognizes the HTML-in-canvas effects and tweets about how it was built; AE3 (covers R6) - re-running the render on a clean checkout produces a frame-identical MP4

---

## Scope Boundaries

- Live-action footage, voice actors, motion-capture - out. Everything is code, screen capture, or HTML-in-canvas.
- Captions / subtitles in-video - out for v1. Add as a separate render in v1.1 if X autoplay needs them.
- Multi-language voiceover or text - out. English-only, US tone.
- Animated 3D logo - out. The wordmark assemble uses 2D HTML-in-canvas, not three.js.
- A 90-second cinematic version - out. The 45s hero plus 15s cutdown is the v1 deliverable.

### Deferred to Follow-Up Work

- 15s cutdown frame-range tuning beyond the obvious window: iterate after initial post-launch feedback
- Captions burn-in for X autoplay: add post-launch if analytics show muted-watch dominates
- Localized cuts (JP, ES) for global launch: separate plan if `printingpress.dev` adds locale routing
- B-roll of the actual press research phase animation (showing absorb gate working): explore in a v2 long-cut

---

## Context & Research

### Relevant Code and Patterns

- The Printing Press itself: [README](../../README.md) is the source of truth for product positioning, NOI framework, and the three flagship CLIs (ESPN, flight-goat, linear-pp-cli) that anchor the Proof beat
- [printingpress.dev](https://printingpress.dev/) - the live site, used as Solution-beat capture target and CTA destination
- The `/printing-press` and `/ppl` slash command flows in [`.claude/`](../../.claude/) and [`skills/`](../../skills/) - reference for how the agent UI looks during a real run, used as Solution-beat shot list
- The catalog data in [`catalog/`](../../catalog/) - source of truth for "24 CLIs across 17 categories" stat that anchors the Proof beat

### Institutional Learnings

- The README's "How we knew this was real" section (gogcli vs Google Workspace CLI) is the proof beat in prose form - the video compresses this into a 3s shot of star counts collapsing into one preference. Translates the "breadth doesn't beat depth" claim into a visual.
- The NOI framework ("Linear isn't an issue tracker, it's a team behavior observatory") is the conceptual hook for a possible alternate cut. Out of scope for v1 because it requires copy delivery the visual-only edit cannot carry, but documented here so a v2 voiceover cut has a north star.

### External References

- Remotion HTML-in-canvas docs: [https://www.remotion.dev/docs/html-in-canvas](https://www.remotion.dev/docs/html-in-canvas) - canonical API surface, lifecycle (`onInit` / `onPaint`), Chrome Canary 149 + `chrome://flags/#canvas-draw-element` flag requirement, no-nesting rule
- Remotion Agent Skills: `npx skills add remotion-dev/skills` - the 28-file rule set Claude Code loads to write correct Remotion. Mandatory install before any prompt.
- HTML-in-canvas best-practices skill: `rules/html-in-canvas.md` inside the skill bundle - covers `--gl=angle` render flag, lifecycle ordering, when to reach for `<HtmlInCanvas>` vs plain DOM
- Remotion v4.0.455+ is required for local `npx remotion render`, Lambda, and Vercel server-side rendering of HTML-in-canvas; bundles a compiled Chrome Canary with the flag pre-enabled
- Launch video formula reference: Hook (0-3s pain, not product name) / Problem (3-10s cost of inaction) / Solution (10-25s product in action) / Proof (25-38s one specific stat) / CTA (38-45s one action). 45s drives 34% more launch-day signups than 60s for tech announcements (per the [Flowjam 30-day playbook](https://www.flowjam.com/blog/tech-product-announcement-video-2026-30-day-launch-playbook))
- Visual-style references: [Linear](https://linear.app) for dark-mode precision typography and animation-as-speed-proof; [Vercel](https://vercel.com) for animated build-log as product demo; [Anthropic](https://anthropic.com) for restrained/measured tone (no hyperbolic claims, no AI-slop visuals)
- Recent Remotion launch corpus (per the /last30days run that informed this plan): [Maciej Dziuba's 10x'd update](https://www.youtube.com/watch?v=2YcZv-HhnRU), [Riley Brown's Codex+Remotion video](https://www.youtube.com/watch?v=Xtd4DjU9AU8) (70K views), [Sabrina Ramonov's reel](https://www.instagram.com/reel/DXzaFPuFwCI/) (89K views, 10K comments) - the bar for Claude+Remotion launch content this month

---

## Key Technical Decisions

- **Remotion v4.0.455+ with HTML-in-canvas, not plain Composition.** The new primitive is the unfair advantage. We render real Claude Desktop and printingpress.dev as live DOM nodes inside `<HtmlInCanvas>`, then post-process with WebGL shaders for the glitch / press-stamp / ink-bleed effects. A v1 without HTML-in-canvas would be indistinguishable from a generic Remotion video.
- **Standalone `launch-video/` subdirectory inside cli-printing-press, not a separate repo.** Keeps the launch artifact next to the product README, source of truth, and catalog stat. Lets us reference local fixtures (e.g., catalog count) at render time. Has its own `package.json` so the main Go repo is untouched.
- **Render path: local `npx remotion render` on a Mac with Chrome Canary, not Lambda for v1.** Lambda requires uploading the Chrome Canary bundle and configuring HTML-in-canvas server-side; that overhead is wrong for a one-shot launch render. Lambda is on the table for v2 if we need to crank out 50 localized cuts.
- **Audio: licensed track + library SFX, no original score.** The visual is the message. A custom score is scope creep that doesn't move the needle on the 45s arc. Use a track from Musicbed or Artlist with a clear sting at the Solution-beat drop (~10s).
- **One composition with 5 named scenes, not 5 compositions.** Lets us export the 15s cutdown as a frame-range subset of the same composition (`--frames=270-720`) rather than maintaining two timelines. This is also the only way R6 (deterministic re-render) and R7 (cutdown reuses frames) both hold.
- **HTML-in-canvas is used surgically (3 shots), not everywhere.** Most scenes are plain Remotion compositions. HtmlInCanvas is reserved for the three signature moments where DOM-as-canvas matters: tab-thrash glitch, press-stamp transition, wordmark ink-assemble. Overuse degrades render time and bypasses the simplicity payoff.
- **No voiceover in v1.** Text-on-screen carries the script. Voiceover requires a recording pipeline, talent, and a re-record loop on copy changes. The video is built to read silent (text frames + sound design) and watch with sound (music sting + SFX). This is also the X-autoplay-friendly default.
- **Asset capture is a separate first-class unit, not handled inline.** Real Claude Desktop screenshots and `printingpress.dev` recordings are version-controlled in `launch-video/assets/` so the video is reproducible from a clean checkout (R6). Captures use the [pp-agent-capture](https://github.com/) skill and `mcp__claude-in-chrome__gif_creator` for browser flows.

---

## Open Questions

### Resolved During Planning

- **Length: 45s vs 60s vs 90s.** Resolved: 45s. Drives higher launch-day signup rates per the 2026 playbook, fits X autoplay, and forces every shot to earn its frames.
- **Voiceover or no voiceover.** Resolved: no VO for v1. Carry script via text frames; audio is music + SFX.
- **One video or three (hero + cutdown + vertical).** Resolved: one composition, three frame ranges + aspect ratios.
- **Use HTML-in-canvas everywhere or surgically.** Resolved: surgically. Three signature shots only.

### Deferred to Implementation

- Exact music track selection - depends on what Musicbed / Artlist has cleared this month and how it pairs with the cut. Pick during U6 once timing locks.
- Final shot count for the Proof beat (3 CLIs vs 4 vs 5) - depends on how the cuts pace at 30fps. Iterate during U7.
- Whether the Solution-beat command line types out character-by-character or hard-cuts in - depends on how it reads alongside the press-stamp transition. A/B during U6.
- Captions burn-in style for the X autoplay variant - deferred to a v1.1 post-launch pass.

---

## Output Structure

    launch-video/
      package.json                       # Remotion v4.0.455+ project, "launch-video" scripts
      remotion.config.ts                 # browserExecutable for Chrome Canary, allowHtmlInCanvas: true, --gl=angle
      tsconfig.json
      src/
        Root.tsx                         # Composition registration: hero (45s), cutdown (15s), vertical (45s @ 1080x1920), square (45s @ 1080x1080)
        compositions/
          Hero.tsx                       # The single composition; 5 scenes sequenced by <Series>
        scenes/
          Hook.tsx                       # 0-3s: tab thrash, HTML-in-canvas glitch
          Problem.tsx                    # 3-10s: agent confusion montage
          Solution.tsx                   # 10-25s: press prints; HTML-in-canvas press-stamp
          Proof.tsx                      # 25-38s: ESPN / flight-goat / linear quick cuts
          CTA.tsx                        # 38-45s: wordmark ink-assemble + URL
        components/
          PressStamp.tsx                 # HtmlInCanvas wrapper applying stamp/ink-bleed shader
          GlitchOverlay.tsx              # HtmlInCanvas wrapper applying RGB-split glitch shader
          WordmarkInkAssemble.tsx        # HtmlInCanvas wrapper that paints wordmark from ink particles
          TerminalEmu.tsx                # Animated terminal rendering: prompt, typing, output, cursor
          ClaudeDesktopFrame.tsx         # Window-chrome frame for Claude Desktop screenshot stills
          BrowserChromeFrame.tsx         # Window-chrome frame for printingpress.dev screenshot stills
        shaders/
          press-stamp.glsl               # Fragment shader: emboss + ink bleed
          glitch.glsl                    # Fragment shader: RGB channel offset + scanline
          ink-assemble.glsl              # Fragment shader: noise-driven particle gather
        timing/
          schedule.ts                    # Single source of truth: scene boundaries in frames at 30fps
        copy/
          script.ts                      # All on-screen text strings, exported for diffing
      assets/
        captures/
          claude-desktop-empty.png       # Real screenshot: Claude Desktop with skill list
          claude-desktop-printing-press-running.png
          claude-desktop-cli-output.png
          printingpress-dev-hero.png
          printingpress-dev-catalog.png
          printingpress-dev-espn-detail.png
        terminal/
          espn-command.cast              # asciinema recording (or scripted JSON for TerminalEmu)
          flightgoat-command.cast
          linear-command.cast
        audio/
          music.mp3                      # Licensed track, drop locked to frame 300 (10s)
          sfx-stamp.wav                  # Press stamp impact, used 3x at scene boundaries
          sfx-glitch.wav                 # Glitch transition swell
          sfx-ink-assemble.wav           # Ink-assemble whoosh + settle
          sfx-keystroke.wav              # Single keystroke for terminal typing
        fonts/
          GeistMono-VariableFont.ttf     # Terminal + UI
          Inter-VariableFont.ttf         # Body copy
      out/
        hero-45s.mp4                     # 1920x1080 @ 30fps
        cutdown-15s.mp4                  # 1920x1080 @ 30fps, frames 270-720
        hero-vertical-45s.mp4            # 1080x1920 @ 30fps
        hero-square-45s.mp4              # 1080x1080 @ 30fps
      README.md                          # How to render, asset capture instructions, Chrome Canary setup
      .gitignore                         # node_modules, out/, *.cast intermediate

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

### Scene timeline (45s @ 30fps = 1350 frames)

    Frame  0          90          300                          750            1140    1350
           |   HOOK   | PROBLEM   |        SOLUTION            |     PROOF    | CTA   |
           | 0-3s     | 3-10s     |        10-25s              |    25-38s    | 38-45s|
           | tab thrash| agent confusion | "/printing-press" | 3 CLIs cuts | wordmark
           | HtmlIn   |              | + press stamps        |              | + URL
           | Canvas   |              |   (HtmlInCanvas)      |              | (HtmlInCanvas)
                          ^                                                       ^
                  music drop @ 10s (frame 300)                            CTA hold @ 42s

### The script (visual, no narration)

| Beat | Frames | What viewer sees | What viewer hears | HTML-in-canvas? |
|------|--------|------------------|-------------------|-----------------|
| Hook | 0-90 | Six terminal panes flickering with different CLIs (gh, stripe, linear, hub, slack, sendgrid). RGB glitch tear separates them. Text floats in: "Six tools. Six syntaxes. One agent." | Music ambient + SFX glitch swell building | Yes - GlitchOverlay over a 2x3 grid of TerminalEmu DOM nodes |
| Problem | 90-300 | Single screen: Claude Desktop screenshot zoomed in. The agent is mid-thrash: tabs opening, retries, stale tokens. Text overlay: "Agents waste 60% of their tokens hunting." | Music build, no sting yet, soft typing SFX | No - plain Remotion with screenshot still |
| Solution | 300-750 | Hard cut to black. Single line types out: `/printing-press espn`. Music drops. Press-stamp transition: a paper texture flips up, a real Claude Desktop screenshot stamps onto it. Cuts to terminal showing the press running through phases (Phase 0 → Phase 5). Text: "One command. Every endpoint. Every insight." | Music drop + press-stamp SFX (3x at phase transitions) | Yes - PressStamp wraps the screenshot DOM, applies emboss + ink-bleed |
| Proof | 750-1140 | Three quick cuts, ~130 frames each: (1) ESPN command, output appears with playoff scores + injury report; (2) flight-goat command, Kayak-style table renders with Google Flights stitched in; (3) linear command, "every blocked issue whose blocker has been stuck for a week" returns in 50ms. Lower-third stat per cut: "1 call", "2 sources", "50ms". | Three rhythmic SFX hits per cut, music continues | No - plain Remotion + TerminalEmu |
| CTA | 1140-1350 | Wordmark "THE PRINTING PRESS" assembles from ink particles (HtmlInCanvas). URL fades in below: `printingpress.dev`. Single command line below URL: `go install github.com/mvanhorn/cli-printing-press/v3/cmd/printing-press@latest`. Hold for 4 seconds. | Music outro + ink-assemble whoosh, settle | Yes - WordmarkInkAssemble shader drives particles |

### HTML-in-canvas component pattern

    <HtmlInCanvas
      onInit={(ctx) => loadShader(ctx, '/shaders/press-stamp.glsl')}
      onPaint={(ctx, frame) => {
        ctx.uniform1f('u_time', frame / 30);
        ctx.drawElementImage(domRef.current);
        ctx.drawArrays(...);
      }}
    >
      <ClaudeDesktopFrame src={captures.claudeDesktopPrintingPressRunning} />
    </HtmlInCanvas>

The DOM child is rendered live, captured per-frame via `drawElementImage`, then post-processed by the shader. Lifecycle: `onInit` runs once (load shader, set up uniforms), `onPaint` runs per frame (update uniforms, draw). Critical: do not nest `<HtmlInCanvas>`. Every signature shot uses exactly one wrapper.

### Render command (canonical)

    # Hero 1920x1080
    npx remotion render src/Root.tsx Hero out/hero-45s.mp4 \
      --gl=angle \
      --browser-executable="/path/to/chrome-canary" \
      --concurrency=2

    # Cutdown 15s = frame range 270-720
    npx remotion render src/Root.tsx Hero out/cutdown-15s.mp4 \
      --gl=angle --frames=270-720

    # Vertical / square - separate compositions @ different sizes, same scenes
    npx remotion render src/Root.tsx HeroVertical out/hero-vertical-45s.mp4 --gl=angle
    npx remotion render src/Root.tsx HeroSquare out/hero-square-45s.mp4 --gl=angle

---

## Implementation Units

- U1. **Project scaffolding + Chrome Canary + skills install**

**Goal:** Create a working Remotion v4.0.455+ project at `launch-video/` that renders an empty composition end-to-end on local machine with HTML-in-canvas enabled.

**Requirements:** R6

**Dependencies:** None

**Files:**
- Create: `launch-video/package.json`, `launch-video/remotion.config.ts`, `launch-video/tsconfig.json`, `launch-video/src/Root.tsx` (placeholder), `launch-video/.gitignore`
- Create: `launch-video/README.md` (covers Chrome Canary install + flag enable + skills install + render commands)

**Approach:**
- Run `npx create-video@latest --blank -y launch-video` from `~/cli-printing-press/`
- Pin Remotion to `^4.0.455` (the version that bundles HTML-in-canvas server-side rendering support)
- Run `npx skills add remotion-dev/skills` so Claude Code loads the 28 rule files (including `html-in-canvas.md`) on every prompt
- Configure `remotion.config.ts` with `Config.setChromiumOpenGlRenderer('angle')` and `allowHtmlInCanvas: true`
- Document Chrome Canary 149+ install + `chrome://flags/#canvas-draw-element` enable in README
- Render an empty 45s composition to verify the pipeline before any scene work

**Patterns to follow:**
- Match the cli-printing-press repo's existing tsconfig strictness
- Use `package.json` `engines` field to pin Node 20+

**Test scenarios:**
- Happy path: `npm run render` produces `out/hero-45s.mp4` of the empty composition. Exit 0. File size > 0.
- Edge case: rendering on a machine without Chrome Canary errors out with the README pointer, not a stack trace
- Test expectation: none for the unit tests directory - this is project scaffolding

**Verification:**
- `npx remotion render` completes without HTML-in-canvas warnings
- `npx skills list` includes `remotion-best-practices` and `remotion-html-in-canvas`
- README walks a clean-machine reader from zero to a rendered MP4 in under 15 minutes

---

- U2. **Asset capture: Claude Desktop screenshots, printingpress.dev recordings, terminal captures**

**Goal:** Version-control all real screenshots and terminal recordings used by the video so any clean checkout reproduces it.

**Requirements:** R4, R6

**Dependencies:** U1

**Files:**
- Create: `launch-video/assets/captures/claude-desktop-empty.png`
- Create: `launch-video/assets/captures/claude-desktop-printing-press-running.png`
- Create: `launch-video/assets/captures/claude-desktop-cli-output.png`
- Create: `launch-video/assets/captures/printingpress-dev-hero.png`
- Create: `launch-video/assets/captures/printingpress-dev-catalog.png`
- Create: `launch-video/assets/captures/printingpress-dev-espn-detail.png`
- Create: `launch-video/assets/terminal/espn-command.cast`
- Create: `launch-video/assets/terminal/flightgoat-command.cast`
- Create: `launch-video/assets/terminal/linear-command.cast`
- Create: `launch-video/scripts/capture.md` (how-to-recapture playbook)

**Approach:**
- Use the user's `pp-agent-capture` skill or native macOS `screencapture` for Claude Desktop stills (`-w` window mode, retina)
- Use [Claude in Chrome's gif_creator](https://www.npmjs.com/package/) MCP tool or `screencapture -w` to grab printingpress.dev hero, catalog, and ESPN detail page
- Record terminal sessions with `asciinema rec` for the three flagship CLIs (espn, flight-goat, linear-pp-cli). Save as `.cast` files - deterministic JSON that the TerminalEmu component (U3) replays at frame-rate
- Document recapture commands in `scripts/capture.md` so any future run can refresh the assets without guesswork
- Export each capture at 2x resolution so HTML-in-canvas can downsample without bilinear blur

**Patterns to follow:**
- Same naming convention used by the `cli-printing-press/manuscripts/` directory: lowercase, hyphenated, scope-prefixed
- Commit assets to git LFS if any single PNG exceeds 5MB

**Test scenarios:**
- Happy path: `ls launch-video/assets/captures/*.png` returns 6 files; each opens and is at least 1920x1080
- Happy path: each `.cast` file is valid JSON and replays cleanly with `asciinema play`
- Edge case: capture playbook script run on a teammate machine produces equivalent assets (modulo cursor position)

**Verification:**
- All assets check into git from a clean clone and the build picks them up at compile time
- The `.cast` files contain only the demo commands, not stray shell history
- Claude Desktop screenshots show no PII (token values, real channel names, real customer data)

---

- U3. **Composition shell + scene scheduling + reusable frame components**

**Goal:** Build the single Hero composition with all 5 scenes wired in placeholder form, plus the three reusable frame components (TerminalEmu, ClaudeDesktopFrame, BrowserChromeFrame) that downstream scene units will compose.

**Requirements:** R1, R2

**Dependencies:** U1, U2

**Files:**
- Create: `launch-video/src/compositions/Hero.tsx`
- Create: `launch-video/src/timing/schedule.ts`
- Create: `launch-video/src/copy/script.ts`
- Create: `launch-video/src/components/TerminalEmu.tsx`
- Create: `launch-video/src/components/ClaudeDesktopFrame.tsx`
- Create: `launch-video/src/components/BrowserChromeFrame.tsx`
- Create: `launch-video/src/scenes/Hook.tsx` (placeholder color slate)
- Create: `launch-video/src/scenes/Problem.tsx` (placeholder)
- Create: `launch-video/src/scenes/Solution.tsx` (placeholder)
- Create: `launch-video/src/scenes/Proof.tsx` (placeholder)
- Create: `launch-video/src/scenes/CTA.tsx` (placeholder)
- Modify: `launch-video/src/Root.tsx` (register Hero composition + 3 aspect ratio variants)
- Test: `launch-video/src/timing/schedule.test.ts`

**Approach:**
- `schedule.ts` exports scene boundaries in frames as a single source of truth: `HOOK_END = 90`, `PROBLEM_END = 300`, `SOLUTION_END = 750`, `PROOF_END = 1140`, `CTA_END = 1350`. Every scene reads these values - timing changes in exactly one file
- `script.ts` exports every on-screen text string. Lets us diff copy edits separately from layout
- `Hero.tsx` uses Remotion's `<Series>` to sequence the 5 scenes by frame count
- TerminalEmu replays an asciinema `.cast` file at frame rate. Takes `castPath` and `startFrame` props. Renders a styled terminal pane with prompt + typed text + output
- ClaudeDesktopFrame wraps a screenshot in the Claude Desktop window chrome (rounded corners, traffic-light buttons, blurred background to suggest depth)
- BrowserChromeFrame does the same for printingpress.dev (URL bar reading `printingpress.dev`, slight glow shadow)
- Register 4 compositions in Root.tsx: `Hero` (1920x1080), `HeroVertical` (1080x1920), `HeroSquare` (1080x1080), `Cutdown` (Hero composition with frame range hint - or just rendered with `--frames=270-720`)

**Patterns to follow:**
- Remotion's [Series](https://www.remotion.dev/docs/series) for scene sequencing
- Remotion's [Img](https://www.remotion.dev/docs/img) wrapper (not raw `<img>`) for retina-correct frame rendering

**Test scenarios:**
- Happy path: `npx remotion render Hero` produces 1350 frames @ 30fps. Each scene renders its placeholder background.
- Happy path: schedule.ts exports correct boundaries; sum of scene durations equals 45 * 30
- Edge case: a frame at the boundary (frame 90, 300, 750, 1140) renders correctly without scene overlap
- Test scenario for schedule.test.ts: assert HOOK_END < PROBLEM_END < SOLUTION_END < PROOF_END < CTA_END and CTA_END = 1350

**Verification:**
- `npx remotion preview` shows the 5 placeholder scenes in sequence at correct timing
- All 4 compositions register and preview without runtime errors
- TerminalEmu replays a sample `.cast` file synced to `useCurrentFrame()`

---

- U4. **Hook scene (0-3s) with HTML-in-canvas glitch overlay**

**Goal:** Open the video with the six-pane tab-thrash that lands the pain in 90 frames, using HTML-in-canvas for the RGB-split glitch.

**Requirements:** R2, R3

**Dependencies:** U3

**Files:**
- Modify: `launch-video/src/scenes/Hook.tsx`
- Create: `launch-video/src/components/GlitchOverlay.tsx`
- Create: `launch-video/src/shaders/glitch.glsl`
- Test: `launch-video/src/scenes/Hook.test.tsx`

**Approach:**
- Render a 2x3 grid of TerminalEmu instances, each with a different CLI's output (gh status, stripe charges, linear issues, hub clones, slack messages, sendgrid stats - real fixtures from the press's catalog)
- Wrap the grid in a single `<HtmlInCanvas>` (no nesting allowed). Inside `onPaint`, run `glitch.glsl` which does RGB channel offset + scanline jitter driven by `u_time`
- The glitch intensity ramps with frame: subtle at frame 0, peaking around frame 60, into a hard cut at frame 90
- Text overlay "Six tools. Six syntaxes. One agent." fades in around frame 30, sits until frame 80, exits with the cut
- Use `interpolate` with easing for the text and shader uniform ramps - no linear motion

**Patterns to follow:**
- Remotion's `interpolate` + `Easing` patterns from official tutorials
- HTML-in-canvas onInit / onPaint lifecycle from the `html-in-canvas.md` skill rule

**Test scenarios:**
- Happy path: scene renders 90 frames, no Chrome Canary warnings, glitch ramp is monotonic
- Happy path: each terminal pane shows distinct CLI output (no copy-paste duplication)
- Edge case: glitch shader compiles on first render (no fallback path)
- Edge case: text "Six tools. Six syntaxes. One agent." is legible at frame 60 even with peak glitch
- Integration: HtmlInCanvas renders correctly when nested inside Series (the only allowed nesting)

**Verification:**
- Visual review: the scene reads as "tools fighting each other" not "one tool with effects"
- The cut to frame 91 (Problem scene start) is hard and clean - no glitch bleeds across the boundary

---

- U5. **Problem scene (3-10s): agent confusion montage**

**Goal:** Land the pain that motivates The Printing Press: agents thrash through tabs, docs, retries, and stale tokens because every CLI speaks a different language.

**Requirements:** R2, R4

**Dependencies:** U3

**Files:**
- Modify: `launch-video/src/scenes/Problem.tsx`
- Test: `launch-video/src/scenes/Problem.test.tsx`

**Approach:**
- Single Claude Desktop screenshot center-frame at 60% scale, slight blur on edges
- Animated overlays: "tab opening" indicators (bounce in, stay), error toasts ("401 unauthorized", "rate limited"), retry counters (3, 4, 5)
- Text frames sequence at frames 110, 180, 250: "60% of agent tokens" / "wasted on doc lookups" / "and wrong syntax"
- The overlays clutter progressively, mirroring the agent's actual experience. By frame 290 the screen is dense, primed for the hard cut to black at frame 300
- No HTML-in-canvas here - plain Remotion is right for static screenshot + animated overlays

**Patterns to follow:**
- Remotion `<Sequence>` for stagger-in animations
- Spring physics from `@remotion/animated-text` for the text frame entrances

**Test scenarios:**
- Happy path: scene renders 210 frames (frames 90-300)
- Happy path: text appears in legible sequence; no two overlap at the same z-index unreadably
- Edge case: at frame 299 the screen is dense with overlays; at frame 300 (next scene) the screen is black - the cut is visually maximal
- Edge case: error toast wording matches realistic agent failure modes (401, rate limit, retry) not generic "error"

**Verification:**
- A viewer who has never seen Claude Desktop reads the scene as "this is broken" not "this is impressive"
- Text frames are timed to allow read-pause-read at 30fps (~50 frames per text beat)

---

- U6. **Solution scene (10-25s): the press prints, with HTML-in-canvas press-stamp transition**

**Goal:** The hero beat. Single command lands. Press starts. Real Claude Desktop screenshots stamp onto canvas with paper-emboss effect. The viewer should feel the satisfaction of one command replacing the chaos.

**Requirements:** R2, R3, R4, R5

**Dependencies:** U3, U2

**Files:**
- Modify: `launch-video/src/scenes/Solution.tsx`
- Create: `launch-video/src/components/PressStamp.tsx`
- Create: `launch-video/src/shaders/press-stamp.glsl`
- Test: `launch-video/src/scenes/Solution.test.tsx`

**Approach:**
- Frame 300-330: pure black, then `/printing-press espn` types out character-by-character with keystroke SFX (one keystroke per ~3 frames). Music drops at frame 300.
- Frame 330-345: command holds, cursor blinks, viewer reads. Then [enter] hit registers visually.
- Frame 345-450: PressStamp transition #1. A paper texture pivots up from below frame, captures `claude-desktop-empty.png` mid-flight via HtmlInCanvas + emboss shader, slams down. SFX: stamp impact at frame 450.
- Frame 450-540: terminal output replays from `espn-command.cast` showing the press running through phases (Phase 0: Resolve, Phase 1: Research, Phase 1.5: Absorb, Phase 2: Generate, Phase 3: GOAT, Phase 4: Shipcheck). Text labels each phase.
- Frame 540-630: PressStamp transition #2 - a second screenshot stamps in showing the espn-pp-cli running its first command. Real terminal output reads "Tonight's NBA playoff games..." - the README's flagship example.
- Frame 630-750: text overlay "One command. Every endpoint. Every insight." Hold.
- Press-stamp shader: `press-stamp.glsl` does emboss (height-based normal calculation) + ink-bleed (radial darkening at low displacement), driven by `u_progress` (0 → 1 across the transition window) and `u_pressure` (peaks at frame 450 / 630 stamp-impact).

**Patterns to follow:**
- Remotion `<Sequence>` for the staggered phase-label entrances
- HTML-in-canvas press-stamp pattern from `rules/html-in-canvas.md` (the skill explicitly lists glitch + stamp as the canonical use cases)

**Test scenarios:**
- Happy path: command "/printing-press espn" types out at correct cadence (no character skips, no double-prints)
- Happy path: both PressStamp transitions render without WebGL errors
- Happy path: terminal phase replay matches the README's documented phases (0 / 1 / 1.5 / 2 / 3 / 4)
- Edge case: at frame 300, music drop syncs exactly with command appearance (off-by-one frame is audible)
- Edge case: PressStamp emboss at peak `u_pressure` does not blow out highlights on the screenshot (clip protection)
- Integration: HtmlInCanvas onPaint runs at 30fps without dropping frames - measure by render-time variance per frame

**Verification:**
- Visual review: the scene reads as "press is doing actual work," not "logo animation"
- The two stamp impacts feel physical - SFX hit + visual jolt + slight settle
- Text "One command. Every endpoint. Every insight." lands cleanly, not buried under transition motion

---

- U7. **Proof scene (25-38s): three CLIs, three stats, three rapid cuts**

**Goal:** Land the credibility - this isn't vapor, three real CLIs do specific things. Each cut shows command + output + one stat.

**Requirements:** R2, R5

**Dependencies:** U3, U2

**Files:**
- Modify: `launch-video/src/scenes/Proof.tsx`
- Test: `launch-video/src/scenes/Proof.test.tsx`

**Approach:**
- Three sub-cuts of ~130 frames each. Each cut: terminal command appears (10 frames), output renders (60 frames), lower-third stat zooms in (30 frames), hold (30 frames), cut.
- Cut 1 (frames 750-880): `espn live` - playoff scores + injury report rendered table. Stat: "1 call. Live scores + injuries + lineup news."
- Cut 2 (frames 880-1010): `flight-goat seattle nyc 4 dec24 jan1` - Kayak-style table with Google Flights stitched. Stat: "2 sources. One query."
- Cut 3 (frames 1010-1140): `linear blocked --since 7d` - 5 issues returned in milliseconds. Stat: "50ms. Compound queries no API can answer."
- TerminalEmu replays the `.cast` files from U2 (no fake output - these are actual recordings)
- Lower-third stat uses a colored bar that slides in from left. Color codes: cyan for ESPN, orange for flight-goat, magenta for linear (Linear's brand)
- No HTML-in-canvas here - plain Remotion handles this beat at full bitrate

**Patterns to follow:**
- Remotion's [@remotion/lottie](https://www.remotion.dev/docs/lottie) NOT used here - keep stat overlays as native React for crispness
- Stagger pattern from U5

**Test scenarios:**
- Happy path: each cut renders 130 frames. Total scene = 390 frames (frames 750-1140)
- Happy path: each `.cast` replay completes within its allotted output window
- Happy path: lower-third stats are legible at 1080p and at 480p (X autoplay quality)
- Edge case: if a `.cast` recording is shorter than 60 frames of output, the terminal holds at the last frame instead of looping
- Edge case: text legibility at the magenta-on-dark lower third is contrast-tested for accessibility (4.5:1 minimum)

**Verification:**
- Visual review: the three cuts read as three distinct products, not one product showing three queries
- Pacing: viewer's eye lands on the stat number ("1", "2", "50") within the first 60 frames of each cut
- Stat content matches the README's flagship example bullets exactly (R5 traceability)

---

- U8. **CTA scene (38-45s): wordmark ink-assemble + URL + install command**

**Goal:** Close on the only thing the viewer needs to remember: the URL and the install command. The wordmark assembles from ink particles using HTML-in-canvas to make the ending feel earned.

**Requirements:** R2, R3, R5

**Dependencies:** U3

**Files:**
- Modify: `launch-video/src/scenes/CTA.tsx`
- Create: `launch-video/src/components/WordmarkInkAssemble.tsx`
- Create: `launch-video/src/shaders/ink-assemble.glsl`
- Test: `launch-video/src/scenes/CTA.test.tsx`

**Approach:**
- Frame 1140-1200: black, ink particles scatter from edges with low density. SFX whoosh.
- Frame 1200-1290: particles converge using noise field; wordmark "THE PRINTING PRESS" forms in Geist Mono Black, letterforms appearing right-to-left as ink density crosses threshold per pixel
- Frame 1290-1320: URL `printingpress.dev` fades in below wordmark. Stays.
- Frame 1320-1350: install command `go install github.com/mvanhorn/cli-printing-press/v3/cmd/printing-press@latest` fades in below URL. Stays.
- HtmlInCanvas wraps an off-screen DOM node containing the rendered wordmark text. Shader `ink-assemble.glsl` reads the text mask, generates a noise field, and reveals pixels as `u_progress` (0 → 1) crosses the per-pixel noise threshold. The result: an organic ink-bleed assemble, not a generic fade
- Hold the final frame (1350) so a paused screenshot of the video shows URL + install command + wordmark

**Patterns to follow:**
- Remotion's [interpolate](https://www.remotion.dev/docs/interpolate) with `Easing.bezier(0.4, 0, 0.2, 1)` for the URL fade
- HtmlInCanvas onPaint lifecycle from U4 / U6 (this is the third and final HtmlInCanvas usage)

**Test scenarios:**
- Happy path: scene renders 210 frames (1140-1350). Wordmark fully assembled by frame 1290.
- Happy path: a paused screenshot at frame 1350 contains all three elements (wordmark, URL, install command) and is suitable as a thumbnail
- Edge case: ink-assemble with `u_progress=0` shows pure black; with `u_progress=1` shows fully assembled wordmark; transition is monotonic
- Edge case: the install command line is one un-broken line at 1920 wide - if it would wrap at 1080 vertical, use a shorter form (`go install ...press@latest` with truncation or two-line layout)
- Integration: WordmarkInkAssemble + URL fade + install command fade do not overlap in animation time (each finishes before the next begins) - tested by snapshot at frames 1199, 1289, 1319

**Verification:**
- Final frame (1350) is screenshot-quality - this is the X share preview
- URL is legible at 480p autoplay quality
- Install command is legible at 1080p (acceptable to require pause-to-read at 480p)

---

- U9. **Audio bed: licensed track + SFX library + frame-locked sync**

**Goal:** Marry the visual cuts to a music drop and SFX hits so the video plays well with sound.

**Requirements:** R2

**Dependencies:** U3, U4, U5, U6, U7, U8 (timing must be locked before audio)

**Files:**
- Create: `launch-video/assets/audio/music.mp3` (licensed)
- Create: `launch-video/assets/audio/sfx-stamp.wav`
- Create: `launch-video/assets/audio/sfx-glitch.wav`
- Create: `launch-video/assets/audio/sfx-ink-assemble.wav`
- Create: `launch-video/assets/audio/sfx-keystroke.wav`
- Modify: `launch-video/src/compositions/Hero.tsx` (mount Audio + Sequence-scheduled SFX)

**Approach:**
- License a music track from Musicbed or Artlist with a clear sting/drop ~10s in. The drop must land at frame 300 (Solution scene start) - cut/duck the track so the drop syncs to the command appearance
- Layer SFX via Remotion's `<Audio>` inside `<Sequence>` so each effect plays at exactly its keyframe
- SFX schedule:
  - frame 60: `sfx-glitch.wav` (Hook glitch peak)
  - frame 300: `sfx-keystroke.wav` x6 (one per character of "/printing-press espn")
  - frame 450: `sfx-stamp.wav` (PressStamp #1 impact)
  - frame 630: `sfx-stamp.wav` (PressStamp #2 impact)
  - frame 880, 1010: subtle hit SFX at Proof cut boundaries
  - frame 1140: `sfx-ink-assemble.wav` (CTA whoosh)
- Pre-master the music so peak loudness sits at -14 LUFS (X / Spotify integrated loudness target)
- Document license + track ID in `assets/audio/LICENSE.md`

**Patterns to follow:**
- Remotion's [Audio](https://www.remotion.dev/docs/audio) inside Sequence for triggered SFX
- The [pp-agent-capture skill](skills/pp-agent-capture) loudness conventions - same -14 LUFS target

**Test scenarios:**
- Happy path: rendered MP4 has audio track at 48kHz stereo, no clipping
- Happy path: music drop sample-aligns with frame 300 (within 1/30s = 33ms tolerance)
- Edge case: SFX never overlaps the music drop loud enough to mask it (sidechain duck if needed)
- Edge case: MP4 plays correctly in QuickTime, X autoplay (muted), Chrome `<video>` element

**Verification:**
- Listening test: the video reads cleanly with sound, with a clear drop at the Solution beat
- Muted test: video still works without audio (visual cuts carry the story) - critical for X autoplay

---

- U10. **Render configuration + cutdown variants + ship checklist**

**Goal:** Produce all 4 final outputs (hero 1920x1080, hero 1080x1920, hero 1080x1080, cutdown 15s) with deterministic settings and document the ship checklist.

**Requirements:** R1, R6, R7

**Dependencies:** U4, U5, U6, U7, U8, U9

**Files:**
- Modify: `launch-video/remotion.config.ts` (codec, bitrate, color profile)
- Create: `launch-video/scripts/render-all.sh` (renders all 4 outputs)
- Create: `launch-video/scripts/cutdown-frame-range.md` (documents which frames make the 15s)
- Modify: `launch-video/README.md` (ship checklist + license attribution)

**Approach:**
- Render config: `codec: 'h264'`, `crf: 18` (high quality, near-lossless visually), `pixelFormat: 'yuv420p'` (broad compatibility), `colorSpace: 'bt709'`
- `render-all.sh` runs the 4 commands sequentially; on success, prints output paths and total render time
- 15s cutdown uses `--frames=270-720` (Solution beat + first half of Proof beat - the highest-density 15s of the video, captures the press-stamp moment + ESPN proof). Document this choice in `cutdown-frame-range.md`
- Vertical 1080x1920 and square 1080x1080 use separate compositions in Root.tsx that re-mount the same scenes at different aspect ratios. Letter/pillarbox the press-stamp screenshots; recompose lower-thirds for the new safe areas
- Ship checklist in README:
  - [ ] All assets check in clean from a fresh clone
  - [ ] `npm run render:all` produces 4 MP4s
  - [ ] Hero MP4 plays in QuickTime, X (muted + unmuted), Chrome `<video>`, Safari iOS
  - [ ] Vertical MP4 plays correctly in X mobile feed
  - [ ] Cutdown MP4 stands alone narratively (someone watching only the cutdown understands the product)
  - [ ] License attribution for music track is in `assets/audio/LICENSE.md` and the project README
  - [ ] No PII in any captured screenshot

**Patterns to follow:**
- Remotion's [render config](https://www.remotion.dev/docs/config) with explicit color space (bt709) for broadcast-safe color
- Determinism: pin font versions (no system fallback), pin Chrome Canary version, pin Remotion version

**Test scenarios:**
- Happy path: `npm run render:all` produces 4 MP4s, each within +/-2s of expected duration
- Happy path: re-running the render on a clean checkout produces byte-identical output (R6 verification - hash compare)
- Edge case: vertical and square recompositions have lower-thirds inside safe area (10% margins)
- Edge case: cutdown plays standalone - a viewer who sees only frames 270-720 understands the product without context

**Verification:**
- All 4 MP4s render cleanly
- File hashes are stable across re-renders on the same machine (R6 deterministic render)
- Ship checklist is fully ticked before any tweet

---

## System-Wide Impact

- **Interaction graph:** This is a new `launch-video/` subdirectory inside cli-printing-press. It does not change the Go binary, the skills, the catalog, or any existing CI workflows. The only repo-level interaction is a new entry in `.gitignore` (for `node_modules` and `out/`) and possibly a new GitHub Actions workflow gating large-asset commits via git LFS.
- **Error propagation:** Render failures are local to this subdirectory. CI is not gated on launch-video render.
- **State lifecycle risks:** Asset files in `assets/` are large (PNGs at 2x, audio files, asciinema casts). Use git LFS for any file >5MB to avoid bloating the main repo's clone time. Document the LFS requirement in the project README.
- **API surface parity:** None. This unit produces an MP4, not a code surface.
- **Integration coverage:** End-to-end render is the integration test. Each scene unit also tests its frame range in isolation, but the production check is "does `npm run render:all` produce 4 valid MP4s on a clean clone".
- **Unchanged invariants:** The Go binary, Claude Code plugin, MCP server, and every published CLI in `library/` are untouched. The launch-video work cannot regress any existing functionality. The README's prose product description and the launch video should align - but this plan does not modify the README; if launch-video copy diverges from README, README is the source of truth and launch-video copy is updated in a follow-up.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| HTML-in-canvas API breaks before launch (unstable, Chrome may change/remove) | Pin Remotion v4.0.455 exactly. Pin Chrome Canary 149 build (or use Remotion's bundled Canary). If API breaks, fall back to plain Remotion compositions for U4 / U6 / U8 - the visual quality drops but the 45s arc still works. Document the fallback in U10's ship checklist. |
| Music license blocks publication | Use Musicbed / Artlist with explicit social-media + landing-page rights cleared. Track LICENSE.md committed to repo. Have a backup track shortlisted before render lock. |
| Real Claude Desktop screenshots leak PII | Pre-flight redaction pass in U2: every screenshot manually reviewed for tokens, real customer names, email previews, channel names. Use a fresh demo Claude Desktop profile with synthetic project data only. |
| 45 seconds is not enough for the message | The plan compresses the README's 7,000-word pitch into the proven 5-beat formula at 30fps. If user testing shows confusion, the deferred 90s cinematic cut is the next iteration - not stretching this hero to 60s. The 45s constraint is load-bearing for X autoplay completion. |
| Render time on Mac with HTML-in-canvas is too slow for iteration | Use `--concurrency=2` minimum. Render scene-by-scene during development (Remotion supports composition-level rendering). Reserve full `render:all` for ship verification. |
| Cutdown 15s frame range (270-720) doesn't read standalone | A/B during U10 with at least three candidate frame ranges (270-720, 300-750, 450-900). Pick the one a cold-watch tester recalls most accurately. Document the chosen range. |
| The video is great but printingpress.dev hero doesn't autoplay it | Coordinate with the site's hero rollout - host the video on Mux or Bunny CDN with WebM + MP4 sources, autoplay muted with poster frame from CTA scene's final frame |
| Aspect-ratio recomposition (vertical / square) breaks the press-stamp framing | The press-stamp transitions assume 16:9. Vertical and square require re-blocked stamp framing - cover this in the Vertical / Square composition in U10, not by stretching the hero comp |

---

## Documentation / Operational Notes

- `launch-video/README.md` covers: Chrome Canary install + flag enable, skills install, asset capture playbook reference, render commands, ship checklist
- `launch-video/scripts/capture.md` documents how to recapture all assets on a fresh machine (used if Claude Desktop UI changes between launch and a v2 cut)
- `launch-video/assets/audio/LICENSE.md` carries the music track license attribution
- Post-ship: pin the rendered MP4s to Mux or Bunny CDN; embed on `printingpress.dev` hero; tweet from `@mvanhorn` and `@trevin` accounts with the 15s cutdown
- The video is not subject to the cli-printing-press repo's release-please workflow - it lives outside the Go module surface

---

## Sources & References

- README of [cli-printing-press](../../README.md) - source of truth for product positioning, the 5 flagship CLIs, and the NOI framework
- [printingpress.dev](https://printingpress.dev/) - the live site, capture target for Solution and CTA scenes
- [Remotion HTML-in-canvas docs](https://www.remotion.dev/docs/html-in-canvas) - API surface, lifecycle, Chrome Canary requirement, no-nesting rule
- [Remotion v4.0.455 release notes](https://github.com/remotion-dev/remotion/releases) - HTML-in-canvas server-side rendering support
- [@Remotion launch tweet 2026-05-04](https://x.com/Remotion/status/2013626968386765291) and the HTML-in-canvas follow-up (this is the new primitive shipped today)
- [Maciej Dziuba - Remotion just 10x'd AI Motion Graphics (2026-05-05)](https://www.youtube.com/watch?v=2YcZv-HhnRU) - 32 views, but the technique walkthrough informs the press-stamp shader approach
- [Riley Brown - Codex Just Replaced 1,000 Hours of Video Editing Tutorials](https://www.youtube.com/watch?v=Xdy1vkhSz-M) - 70K views, the bar for Remotion + AI launch content
- [Sabrina Ramonov - Claude + Remotion makes unlimited AI videos](https://www.instagram.com/reel/DXzaFPuFwCI/) - 89K views, viral Remotion launch reference for Instagram cut considerations
- [r/ClaudeAI launch animation thread](https://www.reddit.com/r/ClaudeAI/comments/1syvi3p/i_used_claude_code_remotion_to_generate_my_apps/) - documents the 80%-fast / 20%-polish gap that HTML-in-canvas + the new HTML primitive close
- /last30days research run on "Remotion prompting" (2026-05-04) - 82 items across 6 sources, raw at `~/Documents/Last30Days/remotion-prompting-raw-v3-2026-05-04.md`
- Launch video formula reference: [Flowjam tech-product-announcement playbook](https://www.flowjam.com/blog/tech-product-announcement-video-2026-30-day-launch-playbook) - 45s drives 34% more launch-day signups than 60s
- [Remotion Agent Skills](https://www.remotion.dev/docs/ai/skills) - the 28 rule files Claude Code loads, including `html-in-canvas.md`
