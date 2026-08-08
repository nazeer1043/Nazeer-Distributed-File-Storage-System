package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

const sessionCookieName = "nazeerdfs_session"
const sessionDuration = 8 * time.Hour

// Security Decision:
// Storing SHA-256 hash of "admin123" instead of plaintext to prevent raw password exposure in source code.
// Constant-time comparison is used via crypto/subtle to protect against timing attacks.
// TODO: Replace package-level hash constant with database/environment configuration in future phases.
const (
	adminUsername     = "admin"
	adminPasswordHash = "240be518fabd2724ddb6f04eeb1da5967448d7e831c08c8fa822809f74c720a9" // SHA-256 hash of "admin123"
)

type User struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}

var defaultAdminUser = User{
	Username: "admin",
	Name:     "Administrator",
	Role:     "Cluster Admin",
}

type sessionData struct {
	user      User
	expiresAt time.Time
}

type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]sessionData
}

var globalSessions = &sessionStore{
	sessions: make(map[string]sessionData),
}

func (s *sessionStore) create(user User) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = sessionData{
		user:      user,
		expiresAt: time.Now().Add(sessionDuration),
	}
	return token, nil
}

func (s *sessionStore) get(token string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, exists := s.sessions[token]
	if !exists {
		return User{}, false
	}
	if time.Now().After(data.expiresAt) {
		return User{}, false
	}
	return data.user, true
}

func (s *sessionStore) destroy(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Success bool   `json:"success"`
	User    *User  `json:"user,omitempty"`
	Message string `json:"message,omitempty"`
}

type SessionResponse struct {
	Authenticated bool  `json:"authenticated"`
	User          *User `json:"user,omitempty"`
}

// LoginHandler handles POST /api/login
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(LoginResponse{
			Success: false,
			Message: "Invalid request payload",
		})
		return
	}

	reqHash := sha256.Sum256([]byte(req.Password))
	reqHashHex := hex.EncodeToString(reqHash[:])

	userMatch := subtle.ConstantTimeCompare([]byte(req.Username), []byte(adminUsername)) == 1
	passMatch := subtle.ConstantTimeCompare([]byte(reqHashHex), []byte(adminPasswordHash)) == 1

	if !userMatch || !passMatch {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(LoginResponse{
			Success: false,
			Message: "Invalid username or password",
		})
		return
	}

	token, err := globalSessions.create(defaultAdminUser)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(LoginResponse{
			Success: false,
			Message: "Failed to create session",
		})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(LoginResponse{
		Success: true,
		User:    &defaultAdminUser,
	})
}

// LogoutHandler handles POST /api/logout
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		globalSessions.destroy(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// SessionHandler handles GET /api/session
func SessionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		json.NewEncoder(w).Encode(SessionResponse{
			Authenticated: false,
		})
		return
	}

	user, valid := globalSessions.get(cookie.Value)
	if !valid {
		json.NewEncoder(w).Encode(SessionResponse{
			Authenticated: false,
		})
		return
	}

	json.NewEncoder(w).Encode(SessionResponse{
		Authenticated: true,
		User:          &user,
	})
}

// RequireAuth is a reusable HTTP middleware to protect endpoints using session cookies.
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Unauthorized access",
			})
			return
		}

		_, valid := globalSessions.get(cookie.Value)
		if !valid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Session expired or invalid",
			})
			return
		}

		next(w, r)
	}
}
