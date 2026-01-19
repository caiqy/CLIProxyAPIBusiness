# Model Reference Model ID Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Persist `model_id` from the remote models JSON, switch admin price lookup to query by `model_id`, and backfill missing `model_id` on lookup.

**Architecture:** Add a `model_id` column and indexes to the `models` table, parse `model_id` alongside `model_name`, and update admin lookup to query by `model_id` with a legacy fallback that writes `model_id` when missing. Update the Billing Rules modal to request by `model_id`.

**Tech Stack:** Go (Gin, GORM), React + TypeScript, PostgreSQL/SQLite migrations.

---

### Task 1: Add failing parser assertions for model_id

**Files:**
- Modify: `internal/modelreference/parser_test.go`

**Step 1: Write the failing test**

```go
func TestParseModelsPayload_FallbacksAndExtras(t *testing.T) {
    // existing setup...
    refA := findReference(refs, "Provider X", "Model A")
    if refA.ModelID != "model-a" {
        t.Fatalf("expected model id from payload id")
    }

    refB := findReference(refs, "Provider X", "model-b")
    if refB.ModelID != "model-b" {
        t.Fatalf("expected model id fallback to key")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/modelreference -run TestParseModelsPayload_FallbacksAndExtras`

Expected: FAIL with `refA.ModelID undefined` or struct field missing.

**Step 3: Commit**

```bash
git add internal/modelreference/parser_test.go
git commit -m "test: cover model id parsing"
```

---

### Task 2: Add failing handler tests for model_id lookup and backfill

**Files:**
- Modify: `internal/http/api/admin/handlers/model_references_test.go`

**Step 1: Write the failing tests**

```go
func TestModelReferencePriceProviderMatch(t *testing.T) {
    row := models.ModelReference{
        ProviderName: "OpenAI",
        ModelName: "gpt-4o",
        ModelID: "gpt-4o",
        // prices...
    }
    c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/model-references/price?provider=OpenAI&model_id=gpt-4o", nil)
    // expect 200 and res.Model == "gpt-4o"
}

func TestModelReferencePriceBackfillModelID(t *testing.T) {
    row := models.ModelReference{
        ProviderName: "OpenAI",
        ModelName: "gpt-4o",
        ModelID: "",
        // prices...
    }
    c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/model-references/price?model_id=gpt-4o", nil)
    // expect 200 and DB row updated with ModelID == "gpt-4o"
}

func TestModelReferencePriceMissingModelID(t *testing.T) {
    c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/model-references/price", nil)
    // expect 400
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/http/api/admin/handlers -run TestModelReferencePrice`

Expected: FAIL with handler still expecting `model_name` and no backfill.

**Step 3: Commit**

```bash
git add internal/http/api/admin/handlers/model_references_test.go
git commit -m "test: update model reference price to model id"
```

---

### Task 3: Add ModelID to schema model and parser

**Files:**
- Modify: `internal/models/model_reference.go`
- Modify: `internal/modelreference/parser.go`
- Modify: `internal/modelreference/store.go`

**Step 1: Implement minimal changes**

```go
// internal/models/model_reference.go
ModelID string `gorm:"type:varchar(255);index"` // Model ID from remote payload.
```

```go
// internal/modelreference/parser.go
// add ID to modelPayload
ID string `json:"id"`

// compute modelIDValue
modelIDValue := strings.TrimSpace(model.ID)
if modelIDValue == "" {
    modelIDValue = strings.TrimSpace(modelKey)
}
if modelIDValue == "" {
    continue
}

ref := models.ModelReference{
    ProviderName: providerName,
    ModelName: modelName,
    ModelID: modelIDValue,
    // ...
}
```

```go
// mergeModelReference
if base.ModelID == "" && incoming.ModelID != "" {
    base.ModelID = incoming.ModelID
}
```

```go
// internal/modelreference/store.go
DoUpdates: clause.AssignmentColumns([]string{
    "model_id",
    // existing fields...
})
```

**Step 2: Run tests to verify they pass/fail appropriately**

Run: `go test ./internal/modelreference -run TestParseModelsPayload_FallbacksAndExtras`

Expected: PASS for parser, handler tests still failing.

**Step 3: Commit**

```bash
git add internal/models/model_reference.go internal/modelreference/parser.go internal/modelreference/store.go
git commit -m "feat: persist model id in model references"
```

---

### Task 4: Add model_id column and indexes in migrations

**Files:**
- Modify: `internal/db/migrate.go`

**Step 1: Add columns**

```go
// Postgres
if errAdd := conn.Exec(`
    ALTER TABLE models
    ADD COLUMN IF NOT EXISTS model_id varchar(255)
`).Error; errAdd != nil {
    return fmt.Errorf("db: add models model_id: %w", errAdd)
}

// SQLite (inside migrateSQLite)
if errAdd := conn.Exec(`
    ALTER TABLE models
    ADD COLUMN IF NOT EXISTS model_id varchar(255)
`).Error; errAdd != nil {
    return fmt.Errorf("db: add models model_id: %w", errAdd)
}
```

**Step 2: Add indexes**

```go
CREATE INDEX IF NOT EXISTS idx_models_model_id ON models (model_id)
CREATE INDEX IF NOT EXISTS idx_models_provider_model_id ON models (provider_name, model_id)
```

**Step 3: Run tests to ensure no breakage**

Run: `go test ./internal/modelreference -run TestParseModelsPayload_FallbacksAndExtras`

Expected: PASS

**Step 4: Commit**

```bash
git add internal/db/migrate.go
git commit -m "feat: add model id column and indexes"
```

---

### Task 5: Update admin lookup to query by model_id and backfill

**Files:**
- Modify: `internal/http/api/admin/handlers/model_references.go`

**Step 1: Implement lookup changes**

```go
modelID := strings.TrimSpace(c.Query("model_id"))
if modelID == "" { /* 400 */ }

// provider + model_id
Where("provider_name = ? AND model_id = ?", provider, modelID)

// model_id fallback
Where("model_id = ?", modelID)

// legacy fallback on model_name
Where("model_name = ?", modelID)
// if found and ref.ModelID == "", update model_id
```

Update response `model` field to return `ref.ModelID` when available, otherwise `ref.ModelName`.

**Step 2: Run handler tests to verify they pass**

Run: `go test ./internal/http/api/admin/handlers -run TestModelReferencePrice`

Expected: PASS

**Step 3: Commit**

```bash
git add internal/http/api/admin/handlers/model_references.go
git commit -m "feat: query model reference by model id"
```

---

### Task 6: Update Billing Rules modal request param

**Files:**
- Modify: `web/src/pages/admin/BillingRules.tsx`

**Step 1: Change request param**

```ts
const params = new URLSearchParams({ model_id: trimmed });
```

**Step 2: Commit**

```bash
git add web/src/pages/admin/BillingRules.tsx
git commit -m "feat: request model reference price by model id"
```

---

### Task 7: Full verification

**Step 1: Run all tests**

Run: `go test ./...`

**Step 2: Build (per project rules)**

Run: `go build -o test-output ./cmd/business && rm -f test-output`

