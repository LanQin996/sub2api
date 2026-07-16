package routes

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// RegisterCommonRoutes 注册通用路由（健康检查、状态等）
func RegisterCommonRoutes(r *gin.Engine) {
	publicImagesDir := filepath.Join("data", "public", "images")
	registerPublicImages(r, publicImagesDir)

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
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

func registerPublicImages(r *gin.Engine, publicImagesDir string) {
	if err := os.MkdirAll(publicImagesDir, 0o755); err != nil {
		return
	}

	r.Use(func(c *gin.Context) {
		c.Next()

		// API routes under /images take precedence. Only unmatched GET/HEAD
		// requests may fall back to files generated in data/public/images.
		if c.FullPath() != "" || (c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) {
			return
		}

		const prefix = "/images/"
		if !strings.HasPrefix(c.Request.URL.Path, prefix) {
			return
		}

		relPath := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(c.Request.URL.Path, prefix)))
		if relPath == "." || filepath.IsAbs(relPath) || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
			return
		}

		filePath := filepath.Join(publicImagesDir, relPath)
		info, err := os.Stat(filePath)
		if err != nil || info.IsDir() {
			return
		}

		c.File(filePath)
		c.Abort()
	})
}
