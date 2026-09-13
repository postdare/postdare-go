package handler

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/hellodeveye/postdare-go/internal/config"
	"github.com/hellodeveye/postdare-go/internal/service"
	"github.com/hellodeveye/postdare-go/internal/sse"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Gin panics at registration time when two routes claim the same slot, which
// no other test would catch until the server failed to boot.
func TestRoutesRegisterWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	cfg := &config.Config{}
	hub := sse.NewHub()
	h := &Handler{DB: database, Config: cfg, Service: service.New(database, cfg, hub, zap.NewNop()), Hub: hub}
	RegisterRoutes(gin.New(), h)
}
