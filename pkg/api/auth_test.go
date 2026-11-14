package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yigithankarabulut/distributed-file-storage/config"
	"github.com/yigithankarabulut/distributed-file-storage/db"
)

func TestPasswordHashing(t *testing.T) {
	// Minimum length check
	_, err := HashPassword("short123")
	assert.Error(t, err, "password shorter than 12 chars must fail")

	// Valid hash
	rawPass := "SecurePassword2026!"
	hash, err := HashPassword(rawPass)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	// Check password
	assert.True(t, CheckPassword(hash, rawPass))
	assert.False(t, CheckPassword(hash, "WrongPassword123!"))
	assert.False(t, CheckPassword(hash, ""))
}

func TestApp_LoginHandler_InMemoryFallback(t *testing.T) {
	app := &App{
		DB: &db.DBWrapper{DB: nil, Active: false},
	}

	cfg := &config.Config{
		AdminBootstrapUser:     "admin_auth_test",
		AdminBootstrapPassword: "adminSuperSecretPassword123!",
	}
	BootstrapAdmin(app.DB, cfg)

	// 1. Successful login with bootstrapped credentials
	loginBody, _ := json.Marshal(LoginRequest{
		Username: "admin_auth_test",
		Password: "adminSuperSecretPassword123!",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	rec := httptest.NewRecorder()

	app.LoginHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp LoginResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.NotNil(t, resp.User)
	assert.Equal(t, "admin_auth_test", resp.User.Username)
	assert.Equal(t, "admin", resp.User.Role)

	// Verify session cookie was set with HttpOnly and Lax
	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			sessionCookie = c
			break
		}
	}
	assert.NotNil(t, sessionCookie)
	assert.NotEmpty(t, sessionCookie.Value)
	assert.True(t, sessionCookie.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, sessionCookie.SameSite)

	// 2. Failed login with wrong password
	badPassBody, _ := json.Marshal(LoginRequest{
		Username: "admin_auth_test",
		Password: "wrongpassword123!",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(badPassBody))
	rec = httptest.NewRecorder()
	app.LoginHandler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// 3. Failed login with non-existent user
	nonExistentBody, _ := json.Marshal(LoginRequest{
		Username: "nobody_user_xyz",
		Password: "somepassword123!",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(nonExistentBody))
	rec = httptest.NewRecorder()
	app.LoginHandler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestApp_RateLimitingLockout(t *testing.T) {
	app := &App{
		DB: &db.DBWrapper{DB: nil, Active: false},
	}

	targetUser := "lockout_target_user"
	badBody, _ := json.Marshal(LoginRequest{
		Username: targetUser,
		Password: "wrongPassword123!",
	})

	// Fail 5 times
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(badBody))
		req.RemoteAddr = "192.0.2.1:12345"
		rec := httptest.NewRecorder()
		app.LoginHandler(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	}

	// 6th attempt must be locked out (HTTP 429)
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(badBody))
	req.RemoteAddr = "192.0.2.1:12345"
	rec := httptest.NewRecorder()
	app.LoginHandler(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

func TestApp_RegisterHandler_InMemoryFallback(t *testing.T) {
	app := &App{
		DB: &db.DBWrapper{DB: nil, Active: false},
	}

	// 1. Password too short (< 12)
	shortPassBody, _ := json.Marshal(RegisterRequest{
		Name:     "Short Pass",
		Email:    "short@nazeerdfs.io",
		Password: "shortpass1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(shortPassBody))
	rec := httptest.NewRecorder()
	app.RegisterHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// 2. Successful registration gets role "user"
	regBody, _ := json.Marshal(RegisterRequest{
		Name:     "Test Operator",
		Email:    "operator@nazeerdfs.io",
		Password: "operatorSecurePassword2026!",
	})

	req = httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(regBody))
	rec = httptest.NewRecorder()
	app.RegisterHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var regResp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&regResp)
	require.NoError(t, err)
	assert.True(t, regResp["success"].(bool))

	// Verify user role in memory is strictly "user"
	assert.Equal(t, "user", globalUsers.roles["operator@nazeerdfs.io"])

	// 3. Login immediately using newly registered credentials
	loginBody, _ := json.Marshal(LoginRequest{
		Username: "operator@nazeerdfs.io",
		Password: "operatorSecurePassword2026!",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	rec = httptest.NewRecorder()
	app.LoginHandler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var loginResp LoginResponse
	err = json.NewDecoder(rec.Body).Decode(&loginResp)
	require.NoError(t, err)
	assert.Equal(t, "user", loginResp.User.Role)
}

func TestApp_SessionAndLogout(t *testing.T) {
	app := &App{
		DB: &db.DBWrapper{DB: nil, Active: false},
	}

	cfg := &config.Config{
		AdminBootstrapUser:     "admin_sess_test",
		AdminBootstrapPassword: "adminSuperSecretPassword123!",
	}
	BootstrapAdmin(app.DB, cfg)

	// Login
	loginBody, _ := json.Marshal(LoginRequest{
		Username: "admin_sess_test",
		Password: "adminSuperSecretPassword123!",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	rec := httptest.NewRecorder()
	app.LoginHandler(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			sessionCookie = c
			break
		}
	}
	require.NotNil(t, sessionCookie)

	// Validate session
	req = httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	app.SessionHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var sessResp SessionResponse
	err := json.NewDecoder(rec.Body).Decode(&sessResp)
	require.NoError(t, err)
	assert.True(t, sessResp.Authenticated)
	assert.Equal(t, "admin_sess_test", sessResp.User.Username)

	// Logout
	req = httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	app.LogoutHandler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify session is invalidated
	req = httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	app.SessionHandler(rec, req)
	sessResp = SessionResponse{}
	_ = json.NewDecoder(rec.Body).Decode(&sessResp)
	assert.False(t, sessResp.Authenticated)
}

func TestCSRFMiddleware(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler := CSRFMiddleware(dummyHandler)

	// 1. GET requests should pass through without CSRF token
	getReq := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, getReq)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 2. State-changing POST without CSRF cookie/header should be rejected (403)
	postReq := httptest.NewRequest(http.MethodPost, "/api/files/delete", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, postReq)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// 3. State-changing POST with matching cookie and header should succeed (200)
	csrfToken := "valid-csrf-token-1234567890abcdef"
	postReq = httptest.NewRequest(http.MethodPost, "/api/files/delete", nil)
	postReq.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	postReq.Header.Set("X-CSRF-Token", csrfToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, postReq)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 4. Mismatched token should be rejected (403)
	postReq = httptest.NewRequest(http.MethodPost, "/api/files/delete", nil)
	postReq.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	postReq.Header.Set("X-CSRF-Token", "mismatched-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, postReq)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := SecurityHeadersMiddleware(dummyHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	assert.NotEmpty(t, rec.Header().Get("Content-Security-Policy"))
}
