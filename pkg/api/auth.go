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

type User struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}

type userStore struct {
	mu    sync.RWMutex
	users map[string]string // username -> SHA-256 password hash
	names map[string]string // username -> Display Name
}

var globalUsers = &userStore{
	users: map[string]string{
		"admin": "240be518fabd2724ddb6f04eeb1da5967448d7e831c08c8fa822809f74c720a9", // admin123
	},
	names: map[string]string{
		"admin": "Mohammed Nazeer Ali",
	},
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

type RegisterRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
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

	globalUsers.mu.RLock()
	expectedHash, exists := globalUsers.users[req.Username]
	displayName := globalUsers.names[req.Username]
	globalUsers.mu.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(LoginResponse{
			Success: false,
			Message: "Invalid username or password",
		})
		return
	}

	if subtle.ConstantTimeCompare([]byte(reqHashHex), []byte(expectedHash)) != 1 {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(LoginResponse{
			Success: false,
			Message: "Invalid username or password",
		})
		return
	}

	if displayName == "" {
		displayName = req.Username
	}

	user := User{
		Username: req.Username,
		Name:     displayName,
		Role:     "Cluster Admin",
	}

	token, err := globalSessions.create(user)
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
		User:    &user,
	})
}

// RegisterHandler handles POST /api/register
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Invalid request payload",
		})
		return
	}

	username := req.Email
	if username == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Email is required",
		})
		return
	}

	if len(req.Password) < 3 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Password must be at least 3 characters",
		})
		return
	}

	passHash := sha256.Sum256([]byte(req.Password))
	passHashHex := hex.EncodeToString(passHash[:])

	if req.Name == "" {
		req.Name = username
	}

	globalUsers.mu.Lock()
	globalUsers.users[username] = passHashHex
	globalUsers.names[username] = req.Name
	globalUsers.mu.Unlock()

	newUser := User{
		Username: username,
		Name:     req.Name,
		Role:     "Cluster Admin",
	}

	token, _ := globalSessions.create(newUser)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"user":    newUser,
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

// RequireAuth protected route middleware
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
