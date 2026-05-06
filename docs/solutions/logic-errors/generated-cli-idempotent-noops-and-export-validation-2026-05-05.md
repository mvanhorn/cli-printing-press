---
title: Generated CLI retry no-ops and export commands require explicit API-shape checks
date: 2026-05-05
category: logic-errors
module: cli-printing-press-generator
problem_type: logic_error
component: tooling
symptoms:
  - "Generated helpers silently swallowed HTTP 409 create conflicts and DELETE 404s"
  - "Generated export was emitted unconditionally and accepted resources without a bare GET collection endpoint"
  - "Export typo arguments fell through to upstream HTTP responses instead of failing as usage errors"
root_cause: missing_validation
resolution_type: code_fix
severity: high
tags:
  - generated-cli
  - idempotency
  - export
  - http-errors
  - json-output
  - generator-templates
---

# Generated CLI retry no-ops and export commands require explicit API-shape checks

## Problem

The Printing Press generator emitted two helper surfaces that treated API state and API shape as implicit:

- Create helpers converted HTTP 409 conflicts into successful no-ops without the caller opting into retry semantics.
- Delete helpers converted HTTP 404 responses into successful no-ops without the caller saying a missing target was acceptable.
- The generic `export` command was generated for API specs that had resources but no bare `GET /<resource>` collection endpoint.

Those behaviors look convenient in one retry path, but they compound poorly across every printed CLI. A real create conflict or delete miss becomes indistinguishable from a successful mutation, and an export command can exist for a resource the API cannot actually export.

## Symptoms

- A generated create command could exit 0 after an upstream HTTP 409, masking "already exists" as success.
- A generated delete command could exit 0 after an upstream HTTP 404, masking "already deleted" as success.
- Under `--json`, the no-op path did not have a structured envelope that machines could distinguish from a normal success payload.
- `export <resource>` accepted arbitrary resource names before checking generated metadata, so typos could fall through into config loading, client setup, and upstream HTTP responses.
- In session history, the Hacker News reprint surfaced the same export-shape bug: `export stories` returned Google sign-in HTML because the Firebase API uses `/topstories.json` and `/item/<id>.json`, not a bare `GET /stories` collection endpoint.

## What Didn't Work

Fixing one printed CLI would not compound. The issue was in the generator templates, so every future CLI needed the safer default.

Helper-level error classification alone was too implicit. The templates could detect "HTTP 409" or "HTTP 404", but without a root flag they had no signal that the caller was intentionally running an idempotent retry flow.

Dogfood evidence alone was noisy. Placeholder-heavy printed CLIs can fail export in several ways; the generator rule needed to be tied to actual endpoint shape, not just one API's confusing response body.

Generic resource-name export overfit the resource inventory. A resource list says what the API talks about; it does not prove the API has a collection endpoint suitable for streaming/export.

## Solution

### 1. Require an explicit root flag before returning a retry no-op

Generated CLIs now expose opt-in root flags for the retry cases:

```go
rootCmd.PersistentFlags().BoolVar(&flags.idempotent, "idempotent", false, "Treat already-existing create results as a successful no-op")
rootCmd.PersistentFlags().BoolVar(&flags.ignoreMissing, "ignore-missing", false, "Treat missing delete targets as a successful no-op")
```

The generated helpers thread the root flags into the error classifiers. A conflict only becomes exit 0 when the user opted in:

```go
case strings.Contains(msg, "HTTP 409"):
	if flags != nil && flags.idempotent {
		return writeNoop(flags, "already_exists", "already exists (no-op)")
	}
	classified := apiErr(err)
	writeAPIErrorEnvelope(flags, classified, ExitCode(classified))
	return classified
```

Delete uses the same shape for missing targets:

```go
if strings.Contains(msg, "HTTP 404") && flags != nil && flags.ignoreMissing {
	return writeNoop(flags, "already_deleted", "already deleted (no-op)")
}
```

With `--json`, those opt-in no-ops emit a machine-readable envelope:

```json
{"status":"noop","reason":"already_deleted","message":"already deleted (no-op)"}
```

### 2. Emit export only for actual bare collection endpoints

The export template is now conditional. It is emitted only when the spec has at least one resource backed by a bare `GET /<resource>` endpoint:

```go
func constrainVisionTemplates(api *spec.APISpec, set VisionTemplateSet) VisionTemplateSet {
	if set.Export && len(exportableResources(api)) == 0 {
		set.Export = false
	}
	return set
}
```

The resource discovery intentionally checks endpoint shape, not just resource names:

```go
if endpoint.Method == "GET" && endpoint.Path == "/"+resourceName {
	resources = append(resources, resourceName)
}
```

### 3. Validate export arguments before config and API work

Generated export commands build a static valid-resource set from the exportable endpoints and reject unknown resources at argument validation time:

```go
validResources := map[string]bool{
	"items": true,
}

if !validResources[resource] {
	return usageErr("unknown export resource %q (valid: %s)", resource, strings.Join(exportableResources, ", "))
}
```

That keeps typos and unsupported resources in CLI-usage territory instead of turning them into auth, config, network, or upstream-content failures.

### 4. Lock the behavior at the generator layer

Cover the new contract with generator/template tests plus goldens:

- `TestGeneratedHelpers_IdempotentNoopsRequireOptIn`
- `TestGeneratedExport_ValidatesResourceArgument`
- `TestGeneratedExport_OmittedWithoutBareCollectionEndpoint`
- `scripts/golden.sh verify`

The tests matter because both fixes are emitted behavior. A passing Go unit test on one helper is not enough if a future template path can silently regenerate the old surface.

## Why This Works

The fix separates "the HTTP request succeeded" from "the caller accepts this retry state." A create conflict and a delete miss remain errors by default; explicit retry flags are the only path that turns them into successful no-ops.

The JSON no-op envelope gives agents a stable branch that is distinct from a normal success payload. They can treat `status: "noop"` as intentional control flow instead of inferring semantics from free-form text or exit code alone.

The export command now mirrors the API contract rather than the resource inventory. If the API has no bare collection endpoint, the generated CLI does not promise a generic export workflow it cannot honor.

Early argument validation also avoids avoidable side effects. When a resource can be validated from generated metadata, do that before loading config, building clients, or making network calls.

## Prevention

- For generated helpers, require an explicit root flag before converting an upstream 4xx into exit 0.
- Gate generated command emission on actual endpoint capability, not inferred resource names.
- Validate arguments backed solely by generated metadata before config, client, auth, or API work.
- When a generator change affects CLI surface or machine-readable output, add focused generator tests and golden coverage in the same PR.
- Use printed-CLI dogfood to decide whether a finding is systemic or API-specific, then land systemic fixes in generator templates.

## Related

- [Issue #599](https://github.com/mvanhorn/cli-printing-press/issues/599) - idempotent helpers respect `--json` and require opt-in no-ops.
- [Issue #593](https://github.com/mvanhorn/cli-printing-press/issues/593) - export validates resource arguments and emits conditionally.
- [Issue #597](https://github.com/mvanhorn/cli-printing-press/issues/597) - retro series that included the retry no-op finding.
- [Issue #586](https://github.com/mvanhorn/cli-printing-press/issues/586) - retro series that included the export-shape finding.
- [PR #635](https://github.com/mvanhorn/cli-printing-press/pull/635) - generator fix for both contracts.
- [HTTP client response cache must invalidate on successful mutations](../design-patterns/http-client-cache-invalidate-on-mutation-2026-05-05.md) - neighboring generator-template mutation-safety pattern.
- [Dry-run is the default for mutator probes in generated test harnesses](../design-patterns/dry-run-default-for-mutator-probes-in-test-harnesses-2026-05-05.md) - sibling pattern for making mutation intent explicit.
