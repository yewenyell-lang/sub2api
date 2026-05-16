package routes

import (
	"net/http"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

var draining atomic.Bool

// SetDraining marks this process as no longer ready for new traffic.
func SetDraining(value bool) {
	draining.Store(value)
}

// IsDraining reports whether this process is draining existing requests.
func IsDraining() bool {
	return draining.Load()
}

// RegisterCommonRoutes 注册通用路由（健康检查、状态等）
func RegisterCommonRoutes(r *gin.Engine) {
	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 就绪检查：蓝绿发布排水时返回 503，避免代理继续分配新请求。
	r.GET("/ready", func(c *gin.Context) {
		if IsDraining() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "draining"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	// Claude Code 遥测日志（忽略，直接返回200）
	r.POST("/api/event_logging/batch", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Setup status endpoint (always returns needs_setup: false in normal mode)
	// This is used by the frontend to detect when the service has restarted after setup
	r.GET("/setup/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"needs_setup": false,
				"step":        "completed",
			},
		})
	})
}
