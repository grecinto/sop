package sopbackend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop"
)

func TestUIRouterRegistersLoginRoute(t *testing.T) {
	templateFS := os.DirFS(filepath.Join("..", "httpserver", "templates"))
	router := NewUIRouter(templateFS, "*.html")

	mux := http.NewServeMux()
	router.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected login page to render with status 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); got == "" {
		t.Fatalf("expected login page body to be rendered")
	}
}

func TestUIRouterRegistersHQLoginRoute(t *testing.T) {
	templateFS := os.DirFS(filepath.Join("..", "httpserver", "templates"))
	router := NewUIRouter(templateFS, "*.html")

	mux := http.NewServeMux()
	router.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/hq-login", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HQ login page to render with status 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "HQ") {
		t.Fatalf("expected HQ login page body to mention HQ, got %q", got)
	}
}

func TestUIRouterFindsTemplatesWhenWorkingDirectoryChanges(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("failed to change working directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(cwd)
	}()

	router := NewUIRouter(nil, "*.html")
	mux := http.NewServeMux()
	router.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected login page to render with status 200 when cwd changes, got %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "SOP Login") {
		t.Fatalf("expected login page body to contain SOP Login, got %q", got)
	}
}

func TestUIRouterRendersLandingPageWithStarterPlanForUser(t *testing.T) {
	oldConfig := config
	config = Config{UsersStore: NewUserStore()}
	defer func() { config = oldConfig }()

	if _, err := config.RegisterUser(context.Background(), "alice", "pw123", "alice@example.com", sop.RoleUser, "starter"); err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	templateFS := os.DirFS(filepath.Join("..", "httpserver", "templates"))
	router := NewUIRouter(templateFS, "*.html")
	mux := http.NewServeMux()
	router.Register(mux)

	store := NewSessionStore(time.Hour)
	oldTokenFacade := tokenFacade
	tokenFacade = store
	defer func() { tokenFacade = oldTokenFacade }()

	accessToken, _, err := store.CreateSession(context.Background(), "alice", sop.RoleUser)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "alice", Roles: []string{sop.RoleUser}}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected landing page to render with status 200, got %d", rec.Code)
	}
	got := rec.Body.String()
	if !strings.Contains(got, "Welcome") {
		t.Fatalf("expected landing page body to contain welcome copy, got %q", got)
	}
	if !strings.Contains(got, "Starter Kit") {
		t.Fatalf("expected landing page body to contain the starter plan, got %q", got)
	}
}

func TestUIRouterShowsDownloadInstallStepAfterSubscription(t *testing.T) {
	oldConfig := config
	config = Config{UsersStore: NewUserStore()}
	defer func() { config = oldConfig }()

	if _, err := config.RegisterUser(context.Background(), "alice", "pw123", "alice@example.com", sop.RoleUser, "starter"); err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	templateFS := os.DirFS(filepath.Join("..", "httpserver", "templates"))
	router := NewUIRouter(templateFS, "*.html")
	mux := http.NewServeMux()
	router.Register(mux)

	store := NewSessionStore(time.Hour)
	oldTokenFacade := tokenFacade
	tokenFacade = store
	defer func() { tokenFacade = oldTokenFacade }()

	accessToken, _, err := store.CreateSession(context.Background(), "alice", sop.RoleUser)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/app/subscribe", strings.NewReader("card_name=Alice&card_number=4242+4242+4242+4242&card_expiry=12%2F30&card_cvc=123"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "alice", Roles: []string{sop.RoleUser}}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected subscription confirmation page to render with status 200, got %d", rec.Code)
	}
	got := rec.Body.String()
	if !strings.Contains(got, "Download the local agent") {
		t.Fatalf("expected subscription page body to contain the download/install step, got %q", got)
	}
	if !strings.Contains(got, "https://github.com/sharedcode/sop/releases") {
		t.Fatalf("expected subscription page body to link to the SOP releases page, got %q", got)
	}
}

func TestUIRouterUnsubscribesUserFromLandingPage(t *testing.T) {
	oldConfig := config
	config = Config{UsersStore: NewUserStore()}
	defer func() { config = oldConfig }()

	if _, err := config.RegisterUser(context.Background(), "alice", "pw123", "alice@example.com", sop.RoleUser, "starter"); err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	templateFS := os.DirFS(filepath.Join("..", "httpserver", "templates"))
	router := NewUIRouter(templateFS, "*.html")
	mux := http.NewServeMux()
	router.Register(mux)

	store := NewSessionStore(time.Hour)
	oldTokenFacade := tokenFacade
	tokenFacade = store
	defer func() { tokenFacade = oldTokenFacade }()

	accessToken, _, err := store.CreateSession(context.Background(), "alice", sop.RoleUser)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/app/unsubscribe", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "alice", Roles: []string{sop.RoleUser}}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected unsubscribe action to render landing page with status 200, got %d", rec.Code)
	}
	got := rec.Body.String()
	if !strings.Contains(got, "unsubscribed") {
		t.Fatalf("expected landing page body to mention the unsubscribed state, got %q", got)
	}

	user, ok, err := config.findUserRecord(context.Background(), "alice")
	if err != nil {
		t.Fatalf("findUserRecord() error = %v", err)
	}
	if !ok || user == nil {
		t.Fatalf("expected alice user record to exist")
	}
	if gotPlan := strings.ToLower(strings.TrimSpace(user.SubscriptionPlan)); gotPlan != "none" {
		t.Fatalf("expected subscription plan to be updated to none, got %q", user.SubscriptionPlan)
	}
}

func TestUIRouterListsSecondPlanAsDisabledForRegularUser(t *testing.T) {
	oldConfig := config
	config = Config{UsersStore: NewUserStore()}
	defer func() { config = oldConfig }()

	if _, err := config.RegisterUser(context.Background(), "alice", "pw123", "alice@example.com", sop.RoleUser, "starter"); err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	templateFS := os.DirFS(filepath.Join("..", "httpserver", "templates"))
	router := NewUIRouter(templateFS, "*.html")
	mux := http.NewServeMux()
	router.Register(mux)

	store := NewSessionStore(time.Hour)
	oldTokenFacade := tokenFacade
	tokenFacade = store
	defer func() { tokenFacade = oldTokenFacade }()

	accessToken, _, err := store.CreateSession(context.Background(), "alice", sop.RoleUser)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "alice", Roles: []string{sop.RoleUser}}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected landing page to render with status 200, got %d", rec.Code)
	}
	got := rec.Body.String()
	if !strings.Contains(got, "HQ") || !strings.Contains(got, "Local Agent") {
		t.Fatalf("expected landing page body to list the second plan, got %q", got)
	}
	if !strings.Contains(got, "Currently unavailable") {
		t.Fatalf("expected landing page body to mark the second plan as unavailable, got %q", got)
	}
}

func TestUIRouterRendersHQPreviewPageForAdmin(t *testing.T) {
	oldConfig := config
	config = Config{UsersStore: NewUserStore()}
	defer func() { config = oldConfig }()

	if _, err := config.RegisterUser(context.Background(), "root", "pw123", "root@example.com", sop.RoleSystemAdmin, "starter"); err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	templateFS := os.DirFS(filepath.Join("..", "httpserver", "templates"))
	router := NewUIRouter(templateFS, "*.html")
	mux := http.NewServeMux()
	router.Register(mux)

	store := NewSessionStore(time.Hour)
	oldTokenFacade := tokenFacade
	tokenFacade = store
	defer func() { tokenFacade = oldTokenFacade }()

	accessToken, _, err := store.CreateSession(context.Background(), "root", sop.RoleSystemAdmin)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/app/hq", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "root", Roles: []string{sop.RoleSystemAdmin}}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HQ preview page to render with status 200, got %d", rec.Code)
	}
	got := rec.Body.String()
	if !strings.Contains(got, "HQ Workspace Preview") {
		t.Fatalf("expected HQ preview page body to contain the preview title, got %q", got)
	}
}

func TestUIRouterRendersAdminCapabilitiesForSystemAdmin(t *testing.T) {
	oldConfig := config
	config = Config{UsersStore: NewUserStore()}
	defer func() { config = oldConfig }()

	if _, err := config.RegisterUser(context.Background(), "root", "pw123", "root@example.com", sop.RoleSystemAdmin, "starter"); err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}

	templateFS := os.DirFS(filepath.Join("..", "httpserver", "templates"))
	router := NewUIRouter(templateFS, "*.html")
	mux := http.NewServeMux()
	router.Register(mux)

	store := NewSessionStore(time.Hour)
	oldTokenFacade := tokenFacade
	tokenFacade = store
	defer func() { tokenFacade = oldTokenFacade }()

	accessToken, _, err := store.CreateSession(context.Background(), "root", sop.RoleSystemAdmin)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req = req.WithContext(sop.ContextWithAuth(req.Context(), sop.AuthContext{UserID: "root", Roles: []string{sop.RoleSystemAdmin}}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected admin landing page to render with status 200, got %d", rec.Code)
	}
	got := rec.Body.String()
	if !strings.Contains(got, "View all registered users") {
		t.Fatalf("expected admin landing page body to contain user management actions, got %q", got)
	}
}
