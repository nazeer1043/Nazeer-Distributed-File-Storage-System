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

func TestApp_ProfileAndUsersWorkflow(t *testing.T) {
	app := &App{
		DB: &db.DBWrapper{DB: nil, Active: false},
	}

	// Bootstrap an admin user with >= 12 char password
	cfg := &config.Config{
		AdminBootstrapUser:     "admin_profile_test",
		AdminBootstrapPassword: "adminSecurePassword2026!",
	}
	BootstrapAdmin(app.DB, cfg)

	// 1. Log in as admin to get session cookie
	loginBody, _ := json.Marshal(LoginRequest{
		Username: "admin_profile_test",
		Password: "adminSecurePassword2026!",
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

	// 2. GET /api/profile
	profReq := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	profReq.AddCookie(sessionCookie)
	profRec := httptest.NewRecorder()
	app.ProfileHandler(profRec, profReq)
	assert.Equal(t, http.StatusOK, profRec.Code)

	var profResp UserProfileResponse
	err := json.NewDecoder(profRec.Body).Decode(&profResp)
	require.NoError(t, err)
	assert.True(t, profResp.Authenticated)
	assert.Equal(t, "admin_profile_test", profResp.User.Username)

	// 3. POST /api/profile update Name & Org
	updateBody, _ := json.Marshal(ProfileUpdateRequest{
		Name:         "Nazeer Enterprise Admin",
		Organization: "Nazeer DFS Global",
	})
	upReq := httptest.NewRequest(http.MethodPost, "/api/profile", bytes.NewReader(updateBody))
	upReq.AddCookie(sessionCookie)
	upRec := httptest.NewRecorder()
	app.ProfileHandler(upRec, upReq)
	assert.Equal(t, http.StatusOK, upRec.Code)

	// 4. Test Password update with wrong current password
	badPassBody, _ := json.Marshal(ProfileUpdateRequest{
		CurrentPassword: "wrongCurrentPassword123",
		NewPassword:     "newSecurePass2026!",
	})
	badReq := httptest.NewRequest(http.MethodPost, "/api/profile", bytes.NewReader(badPassBody))
	badReq.AddCookie(sessionCookie)
	badRec := httptest.NewRecorder()
	app.ProfileHandler(badRec, badReq)
	assert.Equal(t, http.StatusBadRequest, badRec.Code)

	// 5. Test Password update with correct current password
	goodPassBody, _ := json.Marshal(ProfileUpdateRequest{
		CurrentPassword: "adminSecurePassword2026!",
		NewPassword:     "newSecurePass2026!",
	})
	goodReq := httptest.NewRequest(http.MethodPost, "/api/profile", bytes.NewReader(goodPassBody))
	goodReq.AddCookie(sessionCookie)
	goodRec := httptest.NewRecorder()
	app.ProfileHandler(goodRec, goodReq)
	assert.Equal(t, http.StatusOK, goodRec.Code)

	// 6. Test login with newly updated password
	newLoginBody, _ := json.Marshal(LoginRequest{
		Username: "admin_profile_test",
		Password: "newSecurePass2026!",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(newLoginBody))
	rec2 := httptest.NewRecorder()
	app.LoginHandler(rec2, req2)
	assert.Equal(t, http.StatusOK, rec2.Code)

	// 7. GET /api/users
	usersReq := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	usersRec := httptest.NewRecorder()
	app.UsersListHandler(usersRec, usersReq)
	assert.Equal(t, http.StatusOK, usersRec.Code)

	var users []UserSummary
	err = json.NewDecoder(usersRec.Body).Decode(&users)
	require.NoError(t, err)
	assert.NotEmpty(t, users)
}
