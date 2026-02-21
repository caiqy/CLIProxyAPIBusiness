package usage

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/db"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/logging"
)

func TestHandleUsagePersistsRequestIDFromGinContext(t *testing.T) {
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
	logging.SetGinRequestID(ginCtx, "req-gin-123")

	ctx := context.WithValue(context.Background(), "gin", ginCtx)

	plugin := NewGormUsagePlugin(conn)
	plugin.HandleUsage(ctx, coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-4",
		RequestedAt: time.Now().UTC(),
		Detail: coreusage.Detail{
			InputTokens: 1,
			TotalTokens: 1,
		},
	})

	var row struct {
		RequestID string `gorm:"column:request_id"`
	}
	if errFind := conn.Table("usages").
		Select("request_id").
		Order("id DESC").
		Take(&row).Error; errFind != nil {
		t.Fatalf("query request_id: %v", errFind)
	}

	if row.RequestID != "req-gin-123" {
		t.Fatalf("expected request_id=req-gin-123, got %q", row.RequestID)
	}
}
