package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/hellodeveye/postdare-go/internal/config"
	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/service"
	"github.com/hellodeveye/postdare-go/internal/sse"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func setupBoardArchiveTest(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&model.Board{}, &model.Issue{}, &model.Project{}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.DataDir = t.TempDir()
	hub := sse.NewHub()
	h := &Handler{DB: database, Config: cfg, Service: service.New(database, cfg, hub, zap.NewNop()), Hub: hub}
	router := gin.New()
	router.GET("/boards", h.ListBoards)
	router.PATCH("/boards/:board_id", h.UpdateBoard)
	return database, router
}

func boardNames(t *testing.T, router *gin.Engine, path string) []string {
	t.Helper()
	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
	if res.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", path, res.Code, res.Body.String())
	}
	var body struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(body.Data))
	for _, board := range body.Data {
		names = append(names, board.Name)
	}
	return names
}

func patchBoard(t *testing.T, router *gin.Engine, id uint64, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/boards/"+strconv.FormatUint(id, 10), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("PATCH board %d: status %d: %s", id, res.Code, res.Body.String())
	}
	return res
}

// The boards index answers "where is work happening", so an archived board has
// to leave it -- and still be reachable when asked for by name, because its
// issues keep the identifiers already written into commit messages.
func TestArchivedBoardLeavesTheIndexButStaysListable(t *testing.T) {
	database, router := setupBoardArchiveTest(t)
	for _, name := range []string{"Engineering", "Marketing"} {
		if err := database.Create(&model.Board{Name: name, Key: strings.ToUpper(name[:3])}).Error; err != nil {
			t.Fatal(err)
		}
	}

	patchBoard(t, router, 2, `{"archived":true}`)

	if got := boardNames(t, router, "/boards"); len(got) != 1 || got[0] != "Engineering" {
		t.Fatalf("active boards = %v, want [Engineering]", got)
	}
	if got := boardNames(t, router, "/boards?archived=true"); len(got) != 1 || got[0] != "Marketing" {
		t.Fatalf("archived boards = %v, want [Marketing]", got)
	}
}

// Re-archiving must not move the date, or "archived last March" becomes
// "archived just now" the next time anything touches the board.
func TestReArchivingKeepsTheOriginalDate(t *testing.T) {
	database, router := setupBoardArchiveTest(t)
	archived := time.Now().Add(-72 * time.Hour).UTC().Truncate(time.Second)
	if err := database.Create(&model.Board{Name: "Engineering", Key: "ENG", ArchivedAt: &archived}).Error; err != nil {
		t.Fatal(err)
	}

	patchBoard(t, router, 1, `{"archived":true}`)

	var board model.Board
	if err := database.First(&board, 1).Error; err != nil {
		t.Fatal(err)
	}
	if board.ArchivedAt == nil || !board.ArchivedAt.UTC().Truncate(time.Second).Equal(archived) {
		t.Fatalf("archived_at = %v, want it left at %v", board.ArchivedAt, archived)
	}
}

func TestRestoringABoardClearsTheArchiveDate(t *testing.T) {
	database, router := setupBoardArchiveTest(t)
	archived := time.Now().UTC()
	if err := database.Create(&model.Board{Name: "Engineering", Key: "ENG", ArchivedAt: &archived}).Error; err != nil {
		t.Fatal(err)
	}

	patchBoard(t, router, 1, `{"archived":false}`)

	var board model.Board
	if err := database.First(&board, 1).Error; err != nil {
		t.Fatal(err)
	}
	if board.ArchivedAt != nil {
		t.Fatalf("archived_at = %v, want nil", board.ArchivedAt)
	}
	if got := boardNames(t, router, "/boards"); len(got) != 1 {
		t.Fatalf("active boards = %v, want the restored board back", got)
	}
}
