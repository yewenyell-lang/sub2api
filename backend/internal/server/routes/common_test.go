package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestReadyRouteReflectsDrainingState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetDraining(false)
	t.Cleanup(func() { SetDraining(false) })

	r := gin.New()
	RegisterCommonRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/ready status = %d, want %d", rec.Code, http.StatusOK)
	}

	SetDraining(true)
	req = httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/ready draining status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
