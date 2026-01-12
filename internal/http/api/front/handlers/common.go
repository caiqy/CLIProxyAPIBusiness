package handlers

import "github.com/gin-gonic/gin"

// getUserID extracts the user ID from gin context.
func getUserID(c *gin.Context) uint64 {
	val, exists := c.Get("userID")
	if !exists {
		return 0
	}
	switch v := val.(type) {
	case uint64:
		return v
	case int64:
		return uint64(v)
	case uint:
		return uint64(v)
	case int:
		return uint64(v)
	default:
		return 0
	}
}
