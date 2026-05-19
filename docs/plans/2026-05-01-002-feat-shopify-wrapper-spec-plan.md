---
title: "feat: Curated Shopify wrapper spec and baseline scaffold"
type: feat
status: active
date: 2026-05-01
---

# feat: Curated Shopify wrapper spec and baseline scaffold

## Summary

The next step for Shopify is not "generate the CLI from the raw SDL" and it is not yet the full hand-finished `shopify-pp-cli` from PR-4. The correct intermediate deliverable is a curated Shopify wrapper spec that uses the fetched Admin GraphQL SDL as source-of-truth for field validation, but does not ask the Printing Press to infer a useful CLI surface from the full SDL automatically.

The raw SDL fetch succeeded and is now stored at [catalog/specs/shopify-2026-04.graphql](/Users/cathrynlavery/Developer/mvanhorn/cli-printing-press/catalog/specs/shopify-2026-04.graphql). However, a dry run plus scratch generation proved that direct SDL parsing is not sufficient:

- the generated config defaulted to `BaseURL: "https://shopify.dev"` instead of the required per-shop endpoint
- the generator derived only 4 low-value resources from the full schema
- the generated README and command surface were not usable as a Shopify operator CLI

So the next part needs its own spec: define a small, intentional Shopify wrapper spec that encodes the real endpoint, auth, throttling, and the initial operator-facing resource/query surface. Use that wrapper spec to generate the baseline scaffold that PR-4 can then hand-finish with Bulk Ops and the 4 GOAT commands.

---

## Problem Frame

The Printing Press now has the machine primitives Shopify needed:

- endpoint path modeling
- runtime endpoint template substitution
- Shopify-shaped cost throttling

But those machine fixes do not solve the Shopify spec-shape problem.

Shopify Admin GraphQL is a giant schema-first API with thousands of types and many internal or low-value nodes. The current GraphQL parser can validate SDL and synthesize a technically valid CLI, but it cannot yet infer the specific business surface we want:

- orders
- products and variants
- inventory items and inventory levels
- customers
- fulfillment orders
- bulk operations

The raw SDL is therefore the wrong direct generation input for PR-4. We need a curated wrapper spec that says:

- which resources matter
- which queries back them
- which fields should populate the local store
- which runtime settings the generated client must use

Without that wrapper, PR-4 devolves into hand-editing a mostly useless scaffold instead of using the generator as intended.

---

## Goal

Produce a minimal but real Shopify generation spec and baseline scaffold that:

1. points at the real per-shop Admin GraphQL endpoint
2. uses the correct auth header and env vars
3. opts into the throttling primitive shipped in PR-3
4. defines the initial syncable resource surface explicitly
5. generates a scaffold that is recognizably the start of `shopify-pp-cli`
6. gives PR-4 a clean base for Bulk Ops and GOAT command work

This is the "PR-4A" slice. It is smaller than the full Shopify CLI but large enough to validate the architecture.

---

## Requirements

- R1. Keep the fetched SDL in `cli-printing-press` as the schema source-of-truth for the 2026-04 Admin GraphQL API.
- R2. Do not generate the baseline Shopify CLI directly from the raw SDL alone.
- R3. Create a curated wrapper spec, checked in as YAML, that the generator can consume deterministically.
- R4. The wrapper spec must set:
  - `name: shopify`
  - `display_name: Shopify`
  - `base_url: https://{shop}`
  - `graphql_endpoint_path: /admin/api/{api_version}/graphql.json`
  - `endpoint_template_vars: [shop, api_version]`
  - auth header `X-Shopify-Access-Token`
  - env var `SHOPIFY_ACCESS_TOKEN`
- R5. The wrapper spec must opt into cost throttling with `throttling: { enabled: true, shape: shopify }`.
- R6. The wrapper spec must use the real runtime env vocabulary:
  - `SHOPIFY_SHOP`
  - `SHOPIFY_API_VERSION`
  - `SHOPIFY_ACCESS_TOKEN`
- R7. The initial resource set must be intentionally small and operator-relevant:
  - `orders`
  - `products`
  - `inventory-items`
  - `customers`
  - `fulfillment-orders`
  - `bulk-operations`
- R8. The first baseline scaffold must build and pass the standard mechanical quality gates.
- R9. The first baseline scaffold must emit a sensible config and README:
  - no `https://shopify.dev` default
  - mentions multi-store env vars
  - mentions Admin GraphQL, not generic API-key copy
- R10. The baseline scaffold may still be incomplete functionally; Bulk Ops and GOAT commands are explicitly follow-up work.
- R11. The wrapper spec must be written so that future Shopify API version bumps are localized:
  - SDL file changes
  - wrapper spec field/query adjustments
  - no hardcoded endpoint in generated code
- R12. The work must preserve the "machine vs printed CLI" rule: only encode reusable Shopify generation structure in the machine repo; library-specific hand-finish work stays in `printing-press-library`.

---

## Non-Goals

- Not implementing the 4 GOAT commands yet
- Not implementing Bulk Operations orchestration yet
- Not extracting the commerce archetype yet
- Not teaching the generic SDL parser to infer a perfect Shopify CLI from arbitrary GraphQL schemas
- Not finalizing scorecard support for Shopify bulk-query rules
- Not validating production-scale data coverage yet

---

## Proposed Artifacts

### In `cli-printing-press`

- `catalog/specs/shopify-2026-04.graphql`
  - already fetched
  - remains the raw schema source

- `catalog/specs/shopify-2026-04-wrapper.yaml` or similarly named curated spec
  - new internal YAML wrapper spec for generation
  - hand-authored but backed by the real SDL

- optional `catalog/shopify.yaml`
  - only if we want Shopify available as a first-class catalog entry immediately
  - can be deferred until the wrapper spec is stable

### In `printing-press-library`

- `library/commerce/shopify/`
  - first generated baseline scaffold from the curated wrapper spec
  - baseline only, before Bulk Ops/GOAT hand-finishing

---

## Wrapper Spec Shape

The wrapper spec should be a custom Printing Press YAML spec, not raw SDL.

It should include:

```yaml
name: shopify
display_name: Shopify
description: Ecommerce orders, products, customers, inventory, fulfillment orders, and bulk operations via the Shopify Admin GraphQL API.
cli_description: Operate a Shopify store from the terminal with local sync, analytics, and bulk exports.
version: "2026-04"
base_url: https://{shop}
graphql_endpoint_path: /admin/api/{api_version}/graphql.json
endpoint_template_vars:
  - shop
  - api_version
auth:
  type: api_key
  header: X-Shopify-Access-Token
  env_vars:
    - SHOPIFY_ACCESS_TOKEN
config:
  format: toml
  path: ~/.config/shopify-pp-cli/config.toml
throttling:
  enabled: true
  shape: shopify
resources:
  ...
types:
  ...
```

Important detail: use `api_version`, not bare `version`, so the generated env name is `SHOPIFY_API_VERSION` rather than `SHOPIFY_VERSION`.

---

## Initial Resource Surface

The baseline wrapper spec should not attempt to mirror the whole schema. It should define a small set of high-value resources and list/query paths.

### `orders`

Purpose:
- foundational entity for sync
- required for refund analysis, fulfillment lag, and customer cohorting later

Initial fields:
- `id`
- `name`
- `createdAt`
- `processedAt`
- `displayFinancialStatus`
- `displayFulfillmentStatus`
- `currencyCode`
- `totalPriceSet.shopMoney.amount`
- `totalRefundedSet.shopMoney.amount`
- `customer.id`

### `products`

Purpose:
- product and variant inventory analysis later

Initial fields:
- `id`
- `title`
- `handle`
- `vendor`
- `productType`
- `status`
- variants:
  - `id`
  - `sku`
  - `price`
  - `inventoryItem.id`

### `inventory-items`

Purpose:
- inventory-level sync separate from products to avoid over-deep query shapes

Initial fields:
- `id`
- `sku`
- `tracked`
- levels:
  - `location.id`
  - `location.name`
  - `quantities(names: ["available"])`
  - `updatedAt`

### `customers`

Purpose:
- customer cohorting and lifetime-value analysis later

Initial fields:
- `id`
- `email`
- `firstName`
- `lastName`
- `numberOfOrders`
- `amountSpent.amount`
- `amountSpent.currencyCode`
- `createdAt`
- `updatedAt`

### `fulfillment-orders`

Purpose:
- fulfillment lag and routing analysis later

Initial fields:
- `id`
- `status`
- `requestStatus`
- `assignedLocation.location.id`
- `assignedLocation.location.name`
- `fulfillAt`
- `createdAt`
- `updatedAt`
- `order.id`

### `bulk-operations`

Purpose:
- future home for run/poll/download workflow

Initial baseline:
- may start as a thin generated resource or hand-authored command family later
- should be explicitly reserved in the plan even if not fully generated in the first scaffold

---

## Generation Strategy

Use a two-stage generation model:

1. **Schema acquisition**
   - fetch and pin the real Shopify SDL
   - already complete for `2026-04`

2. **Curated generation**
   - generate the baseline scaffold from the wrapper spec, not the raw SDL

This gives us:

- real Shopify field names and query validation
- deterministic scaffold generation
- a manageable CLI surface

It also avoids forcing the generic GraphQL parser to solve a problem it does not currently solve.

---

## Scope Split Between Repos

### `cli-printing-press`

Owns:
- raw SDL
- curated wrapper spec format and contents
- optional catalog entry
- any machine-level fixes discovered while generating the baseline

Does not own:
- the full hand-finished Shopify CLI
- Bulk Ops implementation details specific to the first printed CLI

### `printing-press-library`

Owns:
- generated baseline Shopify CLI directory
- hand-finished Bulk Ops support
- GOAT commands
- verification against a real store

This split keeps the machine reusable and the printed CLI specific.

---

## Acceptance Criteria

This spec is complete when all of the following are true:

- A curated Shopify wrapper spec exists and is checked in.
- Generating from that wrapper spec produces a scaffold under a scratch dir or library dir.
- The generated config uses:
  - `https://{shop}`
  - `/admin/api/{api_version}/graphql.json`
  - `SHOPIFY_SHOP`
  - `SHOPIFY_API_VERSION`
  - `SHOPIFY_ACCESS_TOKEN`
- The generated scaffold opts into Shopify-shaped throttling.
- The generated scaffold builds and passes the mechanical gates.
- The generated README and config are no longer generic nonsense.
- The baseline resource surface is small but meaningful.

---

## Risks

### Risk 1: wrapper spec drifts from SDL

Mitigation:
- every field/query added to the wrapper spec should be validated against the pinned SDL
- treat the SDL as canonical and the wrapper as the curated projection

### Risk 2: GraphQL custom-spec support is still too weak

Mitigation:
- keep the baseline resource set small
- discover the next machine gap before writing hand-finished library code

### Risk 3: overcommitting to generated endpoint resources

Mitigation:
- baseline scaffold is intentionally narrow
- Bulk Ops and GOAT commands remain hand-finished follow-up work

### Risk 4: store-scoped URL or auth still leaks generic defaults

Mitigation:
- acceptance criteria explicitly require inspection of generated config and README before promoting to the library repo

---

## Open Questions

- Should the curated wrapper spec live in `catalog/specs/` or directly in the eventual library directory during iteration?
  - default recommendation: `catalog/specs/` while the machine is still evolving

- Should Shopify get a catalog entry immediately?
  - recommendation: no, not until the wrapper spec generates a sensible baseline

- Should `bulk-operations` be represented in the wrapper spec as a generated resource or reserved for hand-authored commands only?
  - recommendation: reserve the command family in planning, but do not force it into the first generated baseline if that produces junk

---

## Implementation Order

1. Author the curated wrapper spec from the pinned SDL.
2. Generate a scratch Shopify scaffold from the wrapper spec.
3. Inspect:
   - config
   - endpoint URL shape
   - auth env vars
   - README
   - command surface
4. Fix machine gaps only if they are clearly reusable.
5. Once the baseline is acceptable, generate into `printing-press-library/library/commerce/shopify/`.
6. Start the hand-finished PR-4 work on Bulk Ops and GOAT commands.

---

## Smallest Next Action

Write the curated wrapper spec now and treat that as the next executable task.

That is the highest-leverage move because:

- the raw SDL fetch is complete
- the machine primitives are already landed
- the raw SDL generation path is proven insufficient
- the wrapper spec is the smallest artifact that turns Shopify from research into a real baseline CLI

