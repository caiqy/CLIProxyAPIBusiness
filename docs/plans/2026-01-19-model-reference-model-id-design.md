# Model Reference Lookup By Model ID Design

## Goal
Add `model_id` to the `models` table, populate it from the remote JSON payload, and switch admin pricing lookup to query by `model_id` instead of `model_name`, with a fallback that backfills missing `model_id` on read.

## Background
The admin pricing lookup currently queries by `model_name`. The remote models payload includes a stable `id` field that is not persisted. We need to persist `model_id` alongside `model_name` and use `model_id` for lookups. Existing rows without `model_id` should be backfilled when queried.

## Schema Changes
- Add `model_id` column to the `models` table.
- Add indexes for `model_id` and `(provider_name, model_id)` to support lookup filters.
- Keep the existing primary key on `(provider_name, model_name)` to avoid broad migrations.

## Parsing Rules
- Extend the models JSON parser to capture `model_id`:
  - Prefer the model payload field `id`.
  - Fallback to the model map key if `id` is empty.
- Preserve existing `model_name` behavior:
  - Prefer payload `name`.
  - Fallback to the map key.
- Exclude the `id` field from the `extra` payload as before.

## Storage and Merge Behavior
- Store `model_id` into `ModelReference`.
- During merges, set `ModelID` only when the existing value is empty to preserve current display-name merge semantics.
- Update `StoreReferences` upsert to include `model_id` in the assignment list.

## Admin Lookup Changes
- Endpoint: `GET /v0/admin/model-references/price` uses `model_id` query parameter.
- Query order:
  1) `provider_name + model_id` exact match.
  2) `model_id` exact match across all providers (ordered by provider name).
  3) Fallback to `model_name = model_id` to support legacy rows.
- If fallback (3) hits and the row has empty `model_id`, update that row to the requested `model_id`.
- Response `model` field should return `model_id` when available, otherwise `model_name`.

## Frontend Changes
- Billing Rules modal should call the price endpoint with `model_id` instead of `model_name`.
- Provider inference rules remain unchanged.

## Error Handling
- Missing `model_id` returns 400.
- Not found returns 404.
- Database errors return 500.

## Testing
- Parser tests: verify `model_id` parsing from `id` and from map key fallback.
- Handler tests: verify provider match, provider fallback, missing `model_id` (400), not found (404), and legacy backfill when `model_name` matches `model_id`.

