//go:build !embed

package server

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func configureEmbeddedFrontend(
	_ *gin.Engine,
	_ *service.SettingService,
	_ func(),
) bool {
	return false
}
