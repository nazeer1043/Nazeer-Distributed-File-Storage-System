package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yigithankarabulut/distributed-file-storage/db"
)

func TestApp_LoginHandler_InMemoryFallback(t *testing.T) {
	// App with nil DB (simulating offline MySQL)
	app := &App{
		DB: &db.DBWrapper{DB: nil, Active: false},
	}

	// 1. Successful login with default admin credentials
	loginBody, _ := json.Marshal(LoginRequest{
		Username: "admin",
		Password: "admin123",
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
	assert.Equal(t, "admin", resp.User.Username)

	// Verify session cookie was set
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

	// 2. Failed login with wrong password
	badPassBody, _ := json.Marshal(LoginRequest{
		Username: "admin",
		Password: "wrongpassword",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(badPassBody))
	rec = httptest.NewRecorder()
	app.LoginHandler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// 3. Failed login with non-existent user
	nonExistentBody, _ := json.Marshal(LoginRequest{
		Username: "nobody",
		Password: "somepassword",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(nonExistentBody))
	rec = httptest.NewRecorder()
	app.LoginHandler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestApp_RegisterHandler_InMemoryFallback(t *testing.T) {
	app := &App{
		DB: &db.DBWrapper{DB: nil, Active: false},
	}

	// 1. Successful registration
	regBody, _ := json.Marshal(RegisterRequest{
		Name:     "Test Operator",
		Email:    "operator@nazeerdfs.io",
		Password: "operatorSecurePassword2026",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(regBody))
	rec := httptest.NewRecorder()

	app.RegisterHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var regResp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&regResp)
	require.NoError(t, err)
	assert.True(t, regResp["success"].(bool))

	// 2. Login immediately using newly registered credentials
	loginBody, _ := json.Marshal(LoginRequest{
		Username: "operator@nazeerdfs.io",
		Password: "operatorSecurePassword2026",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	rec = httptest.NewRecorder()
	app.LoginHandler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 3. Validation failure: password too short
	shortPassBody, _ := json.Marshal(RegisterRequest{
		Name:     "Short",
		Email:    "short@nazeerdfs.io",
		Password: "12",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(shortPassBody))
	rec = httptest.NewRecorder()
	app.RegisterHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestApp_SessionAndLogout(t *testing.T) {
	app := &App{
		DB: &db.DBWrapper{DB: nil, Active: false},
	}

	// 1. Session request without cookie creates a valid admin session
	req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	rec := httptest.NewRecorder()
	app.SessionHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var sessResp SessionResponse
	err := json.NewDecoder(rec.Body).Decode(&sessResp)
	require.NoError(t, err)
	assert.True(t, sessResp.Authenticated)
	assert.Equal(t, "admin", sessResp.User.Username)

	// Capture token cookie
	cookies := rec.Result().Cookies()
	require.NotEmpty(t, cookies)
	tokenCookie := cookies[0]

	// 2. Validate session with the returned cookie
	req = httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.AddCookie(tokenCookie)
	rec = httptest.NewRecorder()
	app.SessionHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	sessResp = SessionResponse{}
	err = json.NewDecoder(rec.Body).Decode(&sessResp)
	require.NoError(t, err)
	assert.True(t, sessResp.Authenticated)

	// 3. Logout
	req = httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	req.AddCookie(tokenCookie)
	rec = httptest.NewRecorder()
	app.LogoutHandler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestApp_RequireAuthMiddleware(t *testing.T) {
	app := &App{
		DB: &db.DBWrapper{DB: nil, Active: false},
	}

	handlerCalled := false
	protectedHandler := app.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	rec := httptest.NewRecorder()

	protectedHandler(rec, req)

	assert.True(t, handlerCalled)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestStandalone_AuthHandlersCompatibility(t *testing.T) {
	// Verify that standalone functions work without errors
	loginBody, _ := json.Marshal(LoginRequest{
		Username: "admin",
		Password: "admin123",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	rec := httptest.NewRecorder()
	LoginHandler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/session", nil)
	rec = httptest.NewRecorder()
	SessionHandler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	rec = httptest.NewRecorder()
	LogoutHandler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

