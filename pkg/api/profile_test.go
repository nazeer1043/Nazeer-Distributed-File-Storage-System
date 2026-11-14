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

func TestApp_ProfileAndUsersWorkflow(t *testing.T) {
	app := &App{
		DB: &db.DBWrapper{DB: nil, Active: false},
	}

	// 1. Log in as admin to get session cookie
	loginBody, _ := json.Marshal(LoginRequest{
		Username: "admin",
		Password: "admin123",
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
	assert.Equal(t, "admin", profResp.User.Username)

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
		CurrentPassword: "wrongCurrentPassword",
		NewPassword:     "newPass456",
	})
	badReq := httptest.NewRequest(http.MethodPost, "/api/profile", bytes.NewReader(badPassBody))
	badReq.AddCookie(sessionCookie)
	badRec := httptest.NewRecorder()
	app.ProfileHandler(badRec, badReq)
	assert.Equal(t, http.StatusBadRequest, badRec.Code)

	// 5. Test Password update with correct current password
	goodPassBody, _ := json.Marshal(ProfileUpdateRequest{
		CurrentPassword: "admin123",
		NewPassword:     "admin456",
	})
	goodReq := httptest.NewRequest(http.MethodPost, "/api/profile", bytes.NewReader(goodPassBody))
	goodReq.AddCookie(sessionCookie)
	goodRec := httptest.NewRecorder()
	app.ProfileHandler(goodRec, goodReq)
	assert.Equal(t, http.StatusOK, goodRec.Code)

	// Restore password back to admin123 for other tests
	resetPassBody, _ := json.Marshal(ProfileUpdateRequest{
		CurrentPassword: "admin456",
		NewPassword:     "admin123",
	})
	resReq := httptest.NewRequest(http.MethodPost, "/api/profile", bytes.NewReader(resetPassBody))
	resReq.AddCookie(sessionCookie)
	resRec := httptest.NewRecorder()
	app.ProfileHandler(resRec, resReq)
	assert.Equal(t, http.StatusOK, resRec.Code)

	// 6. GET /api/users
	usersReq := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	usersRec := httptest.NewRecorder()
	app.UsersListHandler(usersRec, usersReq)
	assert.Equal(t, http.StatusOK, usersRec.Code)

	var users []UserSummary
	err = json.NewDecoder(usersRec.Body).Decode(&users)
	require.NoError(t, err)
	assert.NotEmpty(t, users)
}
