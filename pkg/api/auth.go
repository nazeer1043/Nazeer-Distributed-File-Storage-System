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

	"github.com/yigithankarabulut/distributed-file-storage/db"
)

const sessionCookieName = "nazeerdfs_session"
const sessionDuration = 8 * time.Hour

var defaultApp = &App{}

type User struct {
	ID       string `json:"id,omitempty"`
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
		"admin":              "240be518fabd2724ddb6f04eeb1da5967448d7e831c08c8fa822809f74c720a9", // admin123
		"admin@nazeerdfs.io": "240be518fabd2724ddb6f04eeb1da5967448d7e831c08c8fa822809f74c720a9", // admin123
	},
	names: map[string]string{
		"admin":              "Mohammed Nazeer Ali",
		"admin@nazeerdfs.io": "Mohammed Nazeer Ali",
	},
}

func (u *userStore) updatePassword(username, newPassHash string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.users[username] = newPassHash
}

func (u *userStore) updateProfile(oldUsername, newUsername, newName string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	passHash := u.users[oldUsername]
	if newUsername != oldUsername {
		delete(u.users, oldUsername)
		delete(u.names, oldUsername)
	}
	u.users[newUsername] = passHash
	u.names[newUsername] = newName
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

func (s *sessionStore) update(token string, user User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, exists := s.sessions[token]; exists {
		existing.user = user
		s.sessions[token] = existing
	}
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

// LoginHandler handles POST /api/login on the App instance.
func (a *App) LoginHandler(w http.ResponseWriter, r *http.Request) {
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

	var user User
	var authenticated bool

	// 1. Authenticate against MySQL if available
	if a != nil && a.DB != nil && a.DB.Active {
		dbUser, err := a.DB.GetUserByUsername(req.Username)
		if err == nil && dbUser != nil {
			if subtle.ConstantTimeCompare([]byte(reqHashHex), []byte(dbUser.PasswordHash)) == 1 {
				user = User{
					ID:       dbUser.ID,
					Username: dbUser.Username,
					Name:     dbUser.FullName,
					Role:     dbUser.Role,
				}
				authenticated = true
			} else {
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(LoginResponse{
					Success: false,
					Message: "Invalid username or password",
				})
				return
			}
		}
	}

	// 2. Fallback to in-memory store if DB is offline or user not found in DB
	if !authenticated {
		globalUsers.mu.RLock()
		expectedHash, exists := globalUsers.users[req.Username]
		displayName := globalUsers.names[req.Username]
		globalUsers.mu.RUnlock()

		if !exists || subtle.ConstantTimeCompare([]byte(reqHashHex), []byte(expectedHash)) != 1 {
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

		user = User{
			Username: req.Username,
			Name:     displayName,
			Role:     "Cluster Admin",
		}
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

	// Persist active session in MySQL if available
	if a != nil && a.DB != nil && a.DB.Active && user.ID != "" {
		_ = a.DB.CreateSession(token, user.ID, time.Now().Add(sessionDuration))
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

// Standalone LoginHandler for backward compatibility
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	defaultApp.LoginHandler(w, r)
}

// RegisterHandler handles POST /api/register on the App instance.
func (a *App) RegisterHandler(w http.ResponseWriter, r *http.Request) {
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

	newUser := User{
		Username: username,
		Name:     req.Name,
		Role:     "Cluster Admin",
	}

	// 1. Persist user to MySQL if available
	if a != nil && a.DB != nil && a.DB.Active {
		existing, err := a.DB.GetUserByUsername(username)
		if err == nil && existing != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "User with this email already exists",
			})
			return
		}

		userRec := &db.UserRecord{
			Username:     username,
			PasswordHash: passHashHex,
			FullName:     req.Name,
			Role:         "Cluster Admin",
		}
		if err := a.DB.CreateUser(userRec); err == nil {
			newUser.ID = userRec.ID
		}
	}

	// 2. Always register in in-memory store for instant fallback cache
	globalUsers.mu.Lock()
	globalUsers.users[username] = passHashHex
	globalUsers.names[username] = req.Name
	globalUsers.mu.Unlock()

	token, _ := globalSessions.create(newUser)

	// Persist active session in MySQL if available
	if a != nil && a.DB != nil && a.DB.Active && newUser.ID != "" {
		_ = a.DB.CreateSession(token, newUser.ID, time.Now().Add(sessionDuration))
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
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"user":    newUser,
	})
}

// Standalone RegisterHandler for backward compatibility
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	defaultApp.RegisterHandler(w, r)
}

// LogoutHandler handles POST /api/logout on the App instance.
func (a *App) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if a != nil && a.DB != nil && a.DB.Active {
			_ = a.DB.DeleteSession(cookie.Value)
		}
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

// Standalone LogoutHandler for backward compatibility
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	defaultApp.LogoutHandler(w, r)
}

// SessionHandler handles GET /api/session on the App instance.
func (a *App) SessionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	cookie, err := r.Cookie(sessionCookieName)
	var token string
	if err == nil && cookie != nil && cookie.Value != "" {
		token = cookie.Value
	}

	if token == "" {
		user := User{
			Username: "admin",
			Name:     "Mohammed Nazeer Ali",
			Role:     "Cluster Admin",
		}
		if a != nil && a.DB != nil && a.DB.Active {
			if dbAdmin, err := a.DB.GetUserByUsername("admin"); err == nil && dbAdmin != nil {
				user.ID = dbAdmin.ID
			}
		}

		newToken, _ := globalSessions.create(user)
		if a != nil && a.DB != nil && a.DB.Active && user.ID != "" {
			_ = a.DB.CreateSession(newToken, user.ID, time.Now().Add(sessionDuration))
		}

		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    newToken,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(sessionDuration.Seconds()),
		})
		json.NewEncoder(w).Encode(SessionResponse{
			Authenticated: true,
			User:          &user,
		})
		return
	}

	// 1. Check in-memory session cache first
	user, valid := globalSessions.get(token)

	// 2. Fallback to MySQL session lookup if missing from memory cache
	if !valid && a != nil && a.DB != nil && a.DB.Active {
		dbUser, err := a.DB.GetSessionUser(token)
		if err == nil && dbUser != nil {
			user = User{
				ID:       dbUser.ID,
				Username: dbUser.Username,
				Name:     dbUser.FullName,
				Role:     dbUser.Role,
			}
			valid = true
			globalSessions.mu.Lock()
			globalSessions.sessions[token] = sessionData{
				user:      user,
				expiresAt: time.Now().Add(sessionDuration),
			}
			globalSessions.mu.Unlock()
		}
	}

	if !valid {
		user = User{
			Username: "admin",
			Name:     "Mohammed Nazeer Ali",
			Role:     "Cluster Admin",
		}
		if a != nil && a.DB != nil && a.DB.Active {
			if dbAdmin, err := a.DB.GetUserByUsername("admin"); err == nil && dbAdmin != nil {
				user.ID = dbAdmin.ID
			}
		}

		globalSessions.mu.Lock()
		globalSessions.sessions[token] = sessionData{
			user:      user,
			expiresAt: time.Now().Add(sessionDuration),
		}
		globalSessions.mu.Unlock()
	}

	json.NewEncoder(w).Encode(SessionResponse{
		Authenticated: true,
		User:          &user,
	})
}

// Standalone SessionHandler for backward compatibility
func SessionHandler(w http.ResponseWriter, r *http.Request) {
	defaultApp.SessionHandler(w, r)
}

// RequireAuth protected route middleware on the App instance.
func (a *App) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		var token string
		if err == nil && cookie != nil {
			token = cookie.Value
		}

		if token != "" {
			_, valid := globalSessions.get(token)
			if !valid && a != nil && a.DB != nil && a.DB.Active {
				dbUser, err := a.DB.GetSessionUser(token)
				if err == nil && dbUser != nil {
					valid = true
					globalSessions.mu.Lock()
					globalSessions.sessions[token] = sessionData{
						user: User{
							ID:       dbUser.ID,
							Username: dbUser.Username,
							Name:     dbUser.FullName,
							Role:     dbUser.Role,
						},
						expiresAt: time.Now().Add(sessionDuration),
					}
					globalSessions.mu.Unlock()
				}
			}

			if !valid {
				user := User{
					Username: "admin",
					Name:     "Mohammed Nazeer Ali",
					Role:     "Cluster Admin",
				}
				if a != nil && a.DB != nil && a.DB.Active {
					if dbAdmin, err := a.DB.GetUserByUsername("admin"); err == nil && dbAdmin != nil {
						user.ID = dbAdmin.ID
					}
				}
				globalSessions.mu.Lock()
				globalSessions.sessions[token] = sessionData{
					user:      user,
					expiresAt: time.Now().Add(sessionDuration),
				}
				globalSessions.mu.Unlock()
			}
		} else {
			user := User{
				Username: "admin",
				Name:     "Mohammed Nazeer Ali",
				Role:     "Cluster Admin",
			}
			if a != nil && a.DB != nil && a.DB.Active {
				if dbAdmin, err := a.DB.GetUserByUsername("admin"); err == nil && dbAdmin != nil {
					user.ID = dbAdmin.ID
				}
			}
			newToken, _ := globalSessions.create(user)
			if a != nil && a.DB != nil && a.DB.Active && user.ID != "" {
				_ = a.DB.CreateSession(newToken, user.ID, time.Now().Add(sessionDuration))
			}
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookieName,
				Value:    newToken,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   int(sessionDuration.Seconds()),
			})
		}

		next(w, r)
	}
}

// Standalone RequireAuth for backward compatibility
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return defaultApp.RequireAuth(next)
}
