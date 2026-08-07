# Concepts

Shared domain vocabulary for this project — entities, named processes, and status concepts with project-specific meaning. Seeded with core domain vocabulary, then accretes as ce-compound and ce-compound-refresh process learnings; direct edits are fine. Glossary only, not a spec or catch-all.

This file stays **code-free** — it defines what the nouns *mean*. For naming conventions, the disambiguation defaults for overloaded words, and the concrete files, packages, and subcommands behind these concepts, see [`docs/GLOSSARY.md`](docs/GLOSSARY.md).

## The system and its product

### the Printing Press
The generator system this repository *is* — the binary, templates, skills, and verification pipeline that together turn an API into a ready-to-ship CLI.
*Avoid:* "the machine" in user-facing output (skill output, issues, retros, confirmation prompts); it is acceptable only in developer conversation and in-repo code and docs.

### printed CLI
A CLI the Printing Press produces for one target — usually an API, or a physical device when generated from a device spec (a *device-native* CLI). Its installed name carries a `-pp-` infix so it never collides with an official vendor CLI.
*Avoid:* "the CLI" when the generator itself is meant — that is a distinct thing (see Flagged ambiguities).

A printed CLI *wraps* its target; it never reimplements it. Every command either reaches the real API or device, or reads from a local store that a sync populated — hand-rolled responses, canned payloads, and locally synthesized reference data are not printed-CLI behavior.

## Inputs

### spec
The contract that drives generation. Most often an API description — official, or recovered through discovery when none is published — but also a *device spec* for a physical device whose control surface is not HTTP-shaped. A spec may be declared *synthetic* when the CLI deliberately spans more than a single contract; the system then relaxes the checks that assume one source of truth.

### device spec
A spec for a local physical device whose control surface is not HTTP-shaped — the first supported protocol is Bluetooth Low Energy. It preserves device-native evidence (the device's services, characteristics, commands, telemetry, and session needs) rather than forcing the device into REST endpoints.

A device spec also records the device's *protocol contract* — how to talk to it (write acknowledgement, command pacing, connection ceremony, teardown behavior) — its behavioral *quirks*, and the proven *workflow* sequences that operate it end to end. These are confirmed on real hardware during validation, not rediscovered command by command, so generation emits real device control rather than guesswork. Each command also carries a *safety class* describing how risky its effect is.

### API slug
The normalized, lowercase identity derived from a spec's title, used as the stable key for an API across the system. The printed CLI's name is built from the slug but is not the same string — do not use the two interchangeably.

### discovery
Recovering a usable spec when no official one exists, by observing how the target is actually used. For APIs the techniques are browser-sniff and crowd-sniff; for physical devices it is device-sniff. They are complementary, and a sniff can also supplement an official spec with surface the docs miss.

### browser-sniff
Discovery from a single live session: real API traffic is captured through the browser and analyzed into a spec. Sees what one authenticated user's client actually calls.

### crowd-sniff
Discovery from the community: published unofficial clients and packages are mined to learn undocumented endpoints, auth patterns, and rate limits. Community-sourced rather than session-captured.

### device-sniff
Discovery for a physical device: captured device evidence (Bluetooth Low Energy, the first supported protocol) is analyzed into a device spec plus a redacted evidence record. The device-world analog of browser-sniff and crowd-sniff; also called *bluetooth-sniff* for the BLE backend.

## A generation run and its artifacts

### generation run
One end-to-end pass that turns a spec into a printed CLI, identified by a run id. Re-running for the same API produces a new run without overwriting earlier ones.

### brief
The condensed output of the research phase that opens a run — API identity, competitive landscape, data model, and product thesis. The document downstream decisions are made against, not a scratch note.

### manuscript
The immutable archive of one generation run: its research, its proofs (validation results), and any discovery artifacts. Manuscripts accumulate across runs for the same API; the working copy of the latest successful run is the local library.

### runstate
Mutable per-workspace bookkeeping for the current run and its sync cursors. The live counterpart to manuscripts — runstate changes as work proceeds, manuscripts are frozen once written.

### provenance
The self-describing generation metadata recorded on a published CLI — what spec it came from, which Printing Press version built it, when, and under what run — so the artifact traces back to its origin without external records.

## Where printed CLIs live

### local library
The on-machine collection of printed CLIs, one per API slug, holding the working copy of each API's latest successful run. A plain directory, not a version-controlled repository. The default meaning of "library."

### public library
The shared, version-controlled catalog of finished CLIs, organized by category, that the publish workflow contributes to. Always named explicitly; an unqualified "library" never means this one.

## Quality and validation

These four checks differ by *what* they inspect and *when*: quality gates are static and build-time, dogfood is structural and generation-time, verify is behavioral and runtime, doctor runs post-install on the end user's machine.

### quality gates
The fixed set of mechanical, build-time checks every printed CLI must pass — dependency hygiene, a vulnerability scan, vetting, the builds, and the basic self-commands. Static pass/fail, distinct from the behavioral and structural checks below.

### dogfood
Generation-time structural validation of a printed CLI against its source spec — catching dead flags, invalid paths, auth mismatches, and drift between the CLI and its agent surface. It inspects shape; it does not exercise the API live.

### verify
Runtime behavioral testing of a printed CLI: every command is actually run, against the real API read-only or a mock, yielding pass/warn/fail verdicts. The dynamic counterpart to the static gates and structural dogfood; it can also auto-patch some failures.

### doctor
The self-diagnostic a printed CLI ships *to its end users*, run after install to check environment, auth, and connectivity. Distinct from dogfood, which validates at generation time inside the Printing Press.

### shipcheck
The combined verification block that gates publishing — the full battery of structural, behavioral, narrative, and scoring checks run together. Every leg must pass before a printed CLI ships.

### scorecard
A two-tier, weighted quality assessment of a printed CLI — infrastructure quality on one side, domain correctness on the other — combined into a single graded score. Judges whether a CLI clears the quality bar; unlike quality gates, it does not gate the build.
*Avoid:* "score" alone when the graded assessment is meant.

## Improvement and maintenance cycles

These four split on two axes: *what* they improve (the printed CLI vs. the Printing Press itself) and *how much* they redo.

### emboss
A second-pass, full improvement cycle on an already-printed CLI — audit, re-research, rebuild, re-verify, report the delta. Improves the printed CLI; broader than polish, narrower than a from-scratch reprint.

### polish
A targeted fix-up of a printed CLI to get it over the verification bar — narrower than emboss's full cycle. Improves the printed CLI, not the Printing Press.

### reprint
Regenerating an existing printed CLI from scratch under the current Printing Press, carrying prior research, novel features, and post-publish fixes forward as context. Chosen when a Printing Press upgrade would help a CLI more than a manual fix-up would.

### retro
Post-run analysis of *the Printing Press itself*, not the printed CLI — finding systemic improvements to templates, the binary, skills, or verification pipeline so the next CLI comes out better.
*Avoid:* "retro" for fixing the CLI that was just generated — that is polish or emboss.

## Surfaces

### agent-native surface
The principle that every printed CLI exposes two parallel surfaces — a CLI surface for humans and an MCP surface for agents — and that any action a human can take should be reachable by an agent. Operator-ergonomic conveniences are the exception: they stay on the human surface, out of the agent catalog.

### MCP surface
A printed CLI's agent-facing catalog of tools, mirroring its commands so an agent can drive the same actions a person can at the terminal. The agent-facing half of the agent-native surface; the CLI surface is the human-facing half.

## Attribution

### creator
The single, permanent person credited with first getting a printed CLI accepted into the public library. Never reassigned — not on a reprint, not on later contributions.

### contributor
Anyone who later contributes to a printed CLI, accrued as a growing list alongside the permanent creator. Added only through the contribution flow; routine regeneration by someone else never adds them.

## Flagged ambiguities

- *"the machine"* and *"the Printing Press"* name the same system; user-facing output uses "the Printing Press," developer contexts may use either.
- *"the CLI"* defaults to a **printed CLI**, not the generator that produces it — these are distinct concepts.
- *"library"* defaults to the **local library**; the **public library** is always named explicitly.
- *"publish"* has two distinct senses: moving a CLI into the local library, and contributing it to the public library.
- *"catalog"* should be qualified: **public library catalog** for the published CLI index, or **tool catalog** for MCP/tool manifests.
