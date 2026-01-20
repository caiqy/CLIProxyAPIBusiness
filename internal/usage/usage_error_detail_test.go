package usage

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/db"
)

func TestUsageErrorDetailCaptured(t *testing.T) {
	gin.SetMode(gin.TestMode)

	conn, errOpen := db.Open(":memory:")
	if errOpen != nil {
		t.Fatalf("open db: %v", errOpen)
	}
	if errMigrate := db.Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate db: %v", errMigrate)
	}

	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Status(http.StatusBadGateway)
	ginCtx.Set("API_RESPONSE", []byte(`{"error":{"message":"upstream failed"}}`))

	ctx := context.WithValue(context.Background(), "gin", ginCtx)

	plugin := NewGormUsagePlugin(conn)
	plugin.HandleUsage(ctx, coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-4",
		RequestedAt: time.Now().UTC(),
		Failed:      true,
	})

	var row struct {
		ErrorStatusCode sql.NullInt64 `gorm:"column:error_status_code"`
		ErrorDetail     []byte        `gorm:"column:error_detail"`
	}
	if errFind := conn.Table("usages").
		Select("error_status_code, error_detail").
		Order("id DESC").
		Take(&row).Error; errFind != nil {
		t.Fatalf("query error detail: %v", errFind)
	}

	if !row.ErrorStatusCode.Valid || row.ErrorStatusCode.Int64 != http.StatusBadGateway {
		t.Fatalf("expected error_status_code=502, got %v", row.ErrorStatusCode.Int64)
	}

	var payload map[string]any
	if errUnmarshal := json.Unmarshal(row.ErrorDetail, &payload); errUnmarshal != nil {
		t.Fatalf("unmarshal error detail: %v", errUnmarshal)
	}
	if payload["status_code"] != float64(http.StatusBadGateway) {
		t.Fatalf("expected status_code=502")
	}
	if payload["message"] != "upstream failed" {
		t.Fatalf("expected message 'upstream failed'")
	}
}
