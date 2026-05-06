---
title: "Steinberger scorecard scoring system: architecture, conventions, and modification rules"
date: 2026-03-27
last_updated: 2026-05-05
category: best-practices
module: internal/pipeline
problem_type: best_practice
component: tooling
symptoms:
  - "Developers modify scoring dimensions without understanding tier weighting, causing score drift"
  - "New dimensions added without updating tier sum constants (120 for Tier 1, 50 for Tier 2)"
  - "Pattern strings changed without verifying they match actual generated code output"
  - "File discovery helpers misused, causing double-counting or missed files"
root_cause: inadequate_documentation
resolution_type: documentation_update
severity: medium
tags:
  - steinberger
  - scorecard
  - scoring-architecture
  - pattern-matching
  - tier-weighting
  - best-practice
  - calibration
  - unscored-dimensions
---

# Steinberger scorecard: architecture, conventions, and modification rules

## Problem

The Steinberger scorecard in `internal/pipeline/scorecard.go` is an 18-dimension scoring system with non-obvious conventions. Developers modifying scoring functions introduce bugs when they don't understand the system's implicit rules around units, ordering, file scope, and pattern matching limitations.

## Scoring Architecture

### Dimension structure

**Tier 1 -- Infrastructure (12 dimensions, 0-10 each, 120 raw max):**

| Dimension | Max | Primary files checked |
|-----------|-----|----------------------|
| OutputModes | 10 | `root.go`, `helpers.go` |
| Auth | 10 | `config/config.go`, `cli/auth.go`, `client/client.go` |
| ErrorHandling | 10 | `helpers.go`, `client/client.go` |
| TerminalUX | 10 | `helpers.go`, `root.go` |
| README | 10 | `README.md` |
| Doctor | 10 | `doctor.go` |
| AgentNative | 10 | `root.go`, `helpers.go`, command files |
| LocalCache | 10 | `client/client.go`, `store/store.go` |
| Breadth | 10 | All CLI files (excludes `infraAllFiles`) |
| Vision | 10 | `store/store.go`, `root.go`, vision-related files |
| Workflows | 10 | All CLI files (excludes `infraCoreFiles`) |
| Insight | 10 | All CLI files (excludes `infraCoreFiles`) |

**Tier 2 -- Domain Correctness (6 dimensions, varying max, 50 raw max):**

| Dimension | Max | Primary files checked |
|-----------|-----|----------------------|
| PathValidity | 10 | Command files + OpenAPI spec |
| AuthProtocol | 10 | `client/client.go`, `config/config.go` + OpenAPI spec |
| DataPipelineIntegrity | 10 | All CLI files, `store/store.go` |
| SyncCorrectness | 10 | All CLI files |
| TypeFidelity | 5 | Command files |
| DeadCode | 5 | `root.go`, `helpers.go`, other CLI files |

### Total formula

```
tier1Normalized = (tier1Raw * 50) / 120   // scale 0-120 → 0-50
tier2Normalized = (tier2Raw * 50) / 50    // scale 0-50  → 0-50
Total = tier1Normalized + tier2Normalized  // 0-100
```

Each tier contributes exactly 50 points max. If you add a Tier 1 dimension, update the `120` constant. If you add a Tier 2 dimension, update the `50` constant.

### Unscored dimensions

Some dimensions are only valid when the source of truth contains the evidence needed to judge them. For example, `PathValidity` needs OpenAPI paths and `AuthProtocol` needs `securitySchemes`.

If that evidence is missing, the dimension is **unscored**, not mediocre:

```go
type Scorecard struct {
    UnscoredDimensions []string `json:"unscored_dimensions,omitempty"`
}
```

Render unscored dimensions as `N/A`, omit them from gap reports, and remove their max points from the denominator before normalizing the tier:

```go
tier2Max := 50
if sc.IsDimensionUnscored("path_validity") { tier2Max -= 10 }
if sc.IsDimensionUnscored("auth_protocol") { tier2Max -= 10 }
tier2Normalized := (tier2Raw * 50) / tier2Max
```

Do **not** encode "missing evidence" as a midpoint like `5/10`. A midpoint looks neutral in code review but still lowers the final score because it stays inside the denominator. That turns an epistemic unknown into a real product penalty.

### Grade thresholds

| Grade | Threshold |
|-------|-----------|
| A | >= 80 |
| B | >= 65 |
| C | >= 50 |
| D | >= 35 |
| F | < 35 |

## Scoring Function Pattern

Every `score*()` function follows the same shape:

```go
func scoreDimension(dir string) int {
    // 1. Read content from relevant files
    content := readFileContent(filepath.Join(dir, "specific/file.go"))
    // OR for broader search:
    content := readAllGoFiles(filepath.Join(dir, "internal", "cli"))

    // 2. Accumulate points for detected patterns
    score := 0
    if strings.Contains(content, "pattern") { score += N }

    // 3. Cap at dimension max
    if score > 10 { score = 10 }
    return score
}
```

### File discovery helpers

- **`readAllGoFiles(dir)`** -- concatenates ALL `.go` files in a directory. Use when a dimension needs to search across all CLI files (e.g., sync logic may live in any file).
- **`readOtherGoFiles(dir, skip)`** -- concatenates all `.go` files except those in the skip map. Use when checking "everything except the file being analyzed" (e.g., dead code detection).
- **`readFileContent(path)`** -- reads a single file. Use when a dimension checks a specific known file.

### Infrastructure file exclusion

```go
// Core infra -- excluded from workflow/insight scoring.
// These contain shared helpers and framework code, not individual commands.
var infraCoreFiles = map[string]bool{
    "helpers.go": true, "root.go": true, "doctor.go": true, "auth.go": true,
}

// Extended infra -- excludes vision/data-layer commands that have their own
// dedicated dimensions (vision, sync_correctness, etc.)
var infraAllFiles = map[string]bool{
    // infraCoreFiles + export.go, import.go, search.go, sync.go, tail.go, analytics.go
}
```

Don't create local `infra` maps. Use the package-level vars. If you need a new set, define it at package level with a documenting comment.

## Verify Calibration

`RunScorecard` accepts an optional `*VerifyReport`. When provided, it calibrates scores against runtime reality.

### Ordering invariant

```
1. Score all dimensions individually
2. Apply verify-based dimension CAPS (e.g., DataPipelineIntegrity <= 5 if pipeline fails)
3. Sum tiers and compute Total
4. Apply verify-based FLOOR on Total
5. Compute grade from final Total
```

**Caps go before the summation. Floors go after.** Violating this ordering makes Total disagree with the sum of visible dimensions.

### Floor formula

```go
verifyScore := int(verifyReport.PassRate)         // PassRate is 0-100, NOT 0.0-1.0
floor := (verifyScore * 80) / 100                 // 91% verify -> 72 floor
if sc.Steinberger.Total < floor { sc.Total = floor }
```

The floor ensures no CLI with 91% verify pass rate scores below 72. Static analysis catches structural issues; verify catches behavioral issues. When they disagree, verify is authoritative.

## Workflow and Insight Detection

Both dimensions use **prefix lists** and **structural detection**:

- **Prefix lists**: Filenames matching known prefixes (e.g., `stale`, `agenda`, `stats`, `health`) count automatically.
- **Structural detection (workflows)**: Any command file containing `/store`, `store.Open`, or `store.New` is a workflow command -- it uses the data layer.
- **Structural detection (insights)**: A command that uses the store AND has aggregation patterns (`COUNT(`, `SUM(`, `GROUP BY`, `\brate\b`) is an insight command.

### Intentional prefix overlap

Six prefixes appear in both workflow and insight lists: `stale`, `conflicts`, `stats`, `trends`, `health`, `noshow`. This is intentional per the Steinberger visionary research plan, which defines analytics as compound commands. A `stats.go` correctly scores in both dimensions. Do not "fix" this overlap.

## Rules for Modifying scorecard.go

1. **Units: PassRate is 0-100, not 0.0-1.0.** Check the source type's scale before arithmetic. Multiplying a 0-100 value by 100 inflates scores by 100x.

2. **Caps before totals, floors after.** Place new dimension caps before the tier summation. Floors go after.

3. **When broadening file scope, audit every downstream pattern check.** `readAllGoFiles` returns ALL `.go` files concatenated. Patterns that made sense for one file (`{`, `return nil`) will match everything. Each pattern must be valid against the full concatenated content.

4. **Use `Count >= 2` not `Contains` for grep-over-own-source.** When extracting identifiers from source and searching the same content, the definition itself matches. Count >= 2 means definition (1) + at least one call (2+).

5. **Use word-boundary regex `\b` for identifier matching.** `strings.Contains(content, "flags,")` matches `featureFlags,`. Always use `\bidentifier[,)]` when matching Go identifiers in concatenated source.

6. **Gate bonus points on prerequisite signals.** Don't award points for a generic pattern unless a qualifying signal is already present (e.g., `/{` only awards sync points when other sync signals exist).

7. **Workflow/insight prefix overlap is intentional.** Don't partition the lists.

8. **Structural detection complements prefix matching.** Don't rely solely on filename prefixes.

9. **Unknown evidence must become `N/A`, not a midpoint.** If the spec or other authority lacks the data needed to evaluate a dimension, mark it unscored, expose it in `unscored_dimensions`, skip it in gap reports, and remove its max points from the tier denominator.

10. **For AuthProtocol, score runtime emission after using the spec to identify the contract.** OpenAPI `securitySchemes` can model one composed header protocol as multiple same-prefix `apiKey` headers. Expand only signing-style companions, preserve explicit OR alternatives, and verify each required header is assigned in the generated client.

11. **Update tier constants when adding dimensions.** Tier 1 constant is `120` (12 x 10). Tier 2 constant is `50`. Both live in the `RunScorecard` function. If dimensions can become unscored, adjust the runtime denominator too.

12. **Test every scoring function independently.** Each `score*()` function should have fixture-based tests covering: high score, low score, dimension-specific edge cases, and unscored/unknown states for evidence-dependent dimensions.

For detailed examples of bugs caused by violating these rules, see `docs/solutions/logic-errors/scorecard-accuracy-broadened-pattern-matching-2026-03-27.md`.

## Related

- `docs/solutions/logic-errors/scorecard-accuracy-broadened-pattern-matching-2026-03-27.md` -- bug-fix doc with before/after code for 10 specific scoring bugs
- `docs/solutions/logic-errors/scorer-dogfood-composed-header-auth-and-example-continuations-2026-05-05.md` -- composed header-auth scoring and shell-style example-tokenizer bug fixes
- `docs/plans/2026-03-25-feat-visionary-research-phase-plan.md` -- defines the Steinberger vision scoring and workflow/insight semantics
- `docs/plans/2026-03-25-fix-scorecard-too-easy-real-quality-plan.md` -- predecessor plan that redesigned scoring from presence-only to quality-aware
- `skills/printing-press/references/scorecard-patterns.md` -- **STALE**: documents only 9 of 18 dimensions, wrong total range, pre-broadening file assumptions. Needs full rewrite.
