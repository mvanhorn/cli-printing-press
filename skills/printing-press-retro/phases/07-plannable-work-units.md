## Phase 5.5: Plannable work units

Group related findings into coherent work units a planner could pick up directly.

For each "Do" finding or group of related findings:

```markdown
### WU-1: <Title> (from F1, F3, ...)
- **Stable ID:** WU-1 *(preserve this identifier when sorting; dependency edges
  use the stable ID rather than a post-sort array position)*
- **Priority:** P1 / P2 *(max priority among absorbed findings — P1 if any
  absorbed finding is P1, else P2)*
- **Type:** bug / enhancement *(use the deterministic category mapping above;
  `bug` wins for a mixed work unit)*
- **Component:** generator / openapi-parser / spec-parser / scorer / skill
  *(must match one of the five fixed component slugs; drives the `comp:*` label
  applied to the issue when filed)*
- **Goal:** One sentence describing the outcome
- **Target:** <component and area, e.g., "Generator templates in internal/generator/">
- **Acceptance criteria:**
  - positive test: ...
  - negative test: ...
- **Scope boundary:** What this does NOT include
- **Dependencies:** None, unless another work unit or issue is a real
  prerequisite. Explicit prerequisites become native GitHub `blocked-by` /
  `blocking` relationships after issue numbers are known. Encode work-unit
  edges as `WU-2|wu:WU-1` (dependent stable ID first); existing issues use
  `WU-2|issue:123`. The executor validates that every WU ID is known and
  resolves both endpoints by stable ID after priority sorting. Related-area
  context stays prose in `Related issues`.
- **Complexity:** small / medium / large
```

The five fixed component slugs are: `generator` (`internal/generator/`),
`openapi-parser` (`internal/openapi/`), `spec-parser` (`internal/spec/`),
`scorer` (verify / dogfood / scorecard), and `skill` (`skills/printing-press/SKILL.md`).
If a WU genuinely spans two, pick the **primary** one — the
component where the durable fix will land. Pick exactly one; don't multi-label.

**If running from inside the printing-press repo (`IN_REPO=true`):**
Resolve target file paths using Glob and Grep tool invocations on `$REPO_ROOT` to
make work units more precise. E.g., use Glob to find `internal/generator/*.go` files,
Grep to find where sync code is generated.

**If running externally (`IN_REPO=false`):**
Describe target components by name (e.g., "Generator templates in `internal/generator/`")
and acceptance criteria without resolved file paths. The fixer will resolve paths when
they pick up the work.

Next: phases/08-issue-gate.md
