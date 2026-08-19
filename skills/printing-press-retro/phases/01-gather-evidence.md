## Phase 1: Gather evidence

Read all artifacts from the run:

1. **Research brief** — `$RUN_DIR/research/*brief*`
2. **Absorb manifest** — `$RUN_DIR/research/*absorb*`
3. **Shipcheck proof** — `$RUN_DIR/proofs/*shipcheck*`
4. **Build log** — `$RUN_DIR/proofs/*build-log*` (if exists)
5. **Live smoke log** — `$RUN_DIR/proofs/*live-smoke*` (if exists)
6. **The generated CLI** — `$CLI_DIR/` (if available)

Also gather the scorecard, verify pass rate, and dogfood report (from the shipcheck
proof or by re-running the tools if `IN_REPO` is true and the binary is available).

Next: phases/02-mine-the-session.md
