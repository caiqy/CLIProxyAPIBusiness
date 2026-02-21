package handlers

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/util"
	"gorm.io/gorm"
)

type transactionRequestLogResponse struct {
	RequestID      string `json:"request_id"`
	APIRequestRaw  string `json:"api_request_raw"`
	APIResponseRaw string `json:"api_response_raw"`
	SourceFile     string `json:"source_file"`
}

var errTransactionRequestLogNotFound = errors.New("transaction request log not found")

// GetTransactionRequestLog returns raw request/response snippets for a usage request.
func (h *DashboardHandler) GetTransactionRequestLog(c *gin.Context) {
	usageID, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil || usageID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid usage id"})
		return
	}

	var usage models.Usage
	errFind := h.db.WithContext(c.Request.Context()).
		Model(&models.Usage{}).
		Select("id", "request_id").
		First(&usage, usageID).Error
	if errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "usage not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query usage failed"})
		return
	}

	requestID := strings.TrimSpace(usage.RequestID)
	if requestID == "" {
		c.JSON(http.StatusConflict, gin.H{"error": "usage request_id is empty"})
		return
	}

	filePath, fileName, errLogPath := findLatestTransactionRequestLogFile(resolveRequestLogsDir(), requestID)
	if errLogPath != nil {
		if errors.Is(errLogPath, errTransactionRequestLogNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "transaction request log not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load transaction request log failed"})
		return
	}

	rawContent, errRead := os.ReadFile(filePath)
	if errRead != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "read transaction request log failed"})
		return
	}

	requestRaw, hasRequest := extractAPILogSection(string(rawContent), "=== API REQUEST")
	responseRaw, hasResponse := extractAPILogSection(string(rawContent), "=== API RESPONSE")
	if !hasRequest && !hasResponse {
		requestRaw = strings.TrimSpace(string(rawContent))
		responseRaw = ""
	}

	c.JSON(http.StatusOK, transactionRequestLogResponse{
		RequestID:      requestID,
		APIRequestRaw:  requestRaw,
		APIResponseRaw: responseRaw,
		SourceFile:     fileName,
	})
}

func resolveRequestLogsDir() string {
	if base := util.WritablePath(); base != "" {
		return filepath.Join(base, "logs")
	}
	return "logs"
}

func findLatestTransactionRequestLogFile(logsDir string, requestID string) (string, string, error) {
	entries, errReadDir := os.ReadDir(logsDir)
	if errReadDir != nil {
		if os.IsNotExist(errReadDir) {
			if _, errStat := os.Stat(logsDir); os.IsNotExist(errStat) {
				return "", "", errTransactionRequestLogNotFound
			}
		}
		return "", "", errReadDir
	}

	suffix := "-" + requestID + ".log"
	latestPath := ""
	latestName := ""
	var latestModTime int64

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, suffix) {
			continue
		}

		info, errInfo := entry.Info()
		if errInfo != nil {
			continue
		}
		modUnix := info.ModTime().UnixNano()
		if latestPath == "" || modUnix > latestModTime {
			latestModTime = modUnix
			latestName = name
			latestPath = filepath.Join(logsDir, name)
		}
	}

	if latestPath == "" {
		return "", "", errTransactionRequestLogNotFound
	}
	return latestPath, latestName, nil
}

func extractAPILogSection(content string, sectionHeaderPrefix string) (string, bool) {
	lines := strings.Split(content, "\n")
	start := -1
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, sectionHeaderPrefix) && strings.HasSuffix(line, "===") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return "", false
	}

	end := len(lines)
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "===") && strings.HasSuffix(line, "===") {
			end = i
			break
		}
	}

	return strings.TrimSpace(strings.Join(lines[start:end], "\n")), true
}
