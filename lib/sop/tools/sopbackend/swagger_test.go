package sopbackend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterSwaggerHandlersServesOpenAPIAndUI(t *testing.T) {
	mux := http.NewServeMux()
	RegisterSwaggerHandlers(mux, "")

	req := httptest.NewRequest(http.MethodGet, "/swagger.json", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected /swagger.json to return 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "\"openapi\"") {
		t.Fatalf("expected swagger spec payload, got %q", body)
	}
	if body := rec.Body.String(); !strings.Contains(body, "AuthLoginRequest") {
		t.Fatalf("expected swagger spec to include auth schemas, got %q", body)
	}
	if body := rec.Body.String(); !strings.Contains(body, "MonitorabilityInfoResponse") {
		t.Fatalf("expected swagger spec to include monitorability schema, got %q", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/swagger/", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected /swagger/ to return 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Swagger UI") {
		t.Fatalf("expected swagger UI page, got %q", body)
	}
}

func TestRegisterSwaggerHandlersDoesNotAdvertiseSiteSubscriptionEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	RegisterSwaggerHandlers(mux, "")

	req := httptest.NewRequest(http.MethodGet, "/swagger.json", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected /swagger.json to return 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if strings.Contains(body, "\"/api/auth/subscription\"") {
		t.Fatalf("expected swagger spec to omit the site-only subscription endpoint, got %q", body)
	}
	if strings.Contains(body, "SubscriptionUpdateRequest") {
		t.Fatalf("expected swagger spec to omit the site-only subscription schema, got %q", body)
	}
}

func TestRegisterSwaggerHandlersIsIdempotent(t *testing.T) {
	mux := http.NewServeMux()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("expected repeated swagger registration not to panic: %v", r)
		}
	}()

	RegisterSwaggerHandlers(mux, "")
	RegisterSwaggerHandlers(mux, "")

	req := httptest.NewRequest(http.MethodGet, "/swagger.json", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected /swagger.json to return 200 after repeated registration, got %d", rec.Code)
	}
}
