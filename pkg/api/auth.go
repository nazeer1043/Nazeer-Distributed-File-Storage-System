package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/yigithankarabulut/distributed-file-storage/config"
	"github.com/yigithankarabulut/distributed-file-storage/db"
)

const (
	sessionCookieName = "nazeerdfs_session"
	csrfCookieName    = "nazeerdfs_csrf"
	sessionDuration   = 8 * time.Hour
	MinPasswordLength = 12
)

var defaultApp = &App{}

type User struct {
	ID       string `json:"id,omitempty"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Role     string `json:"role"` // "admin" or "user"
}

// HashPassword hashes a plain-text password using bcrypt with a minimum length check of 12.
func HashPassword(password string) (string, error) {
	if len(password) < MinPasswordLength {
		return "", fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword securely compares a bcrypt hash against a candidate plain-text password.
func CheckPassword(storedHash, password string) bool {
	if storedHash == "" || password == "" {
		return false
	}
	// Verify with bcrypt
	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)); err == nil {
		return true
	}
	// Constant-time fallback for legacy SHA-256 hashes during migration
	reqHash := sha256.Sum256([]byte(password))
	reqHashHex := hex.EncodeToString(reqHash[:])
	return subtle.ConstantTimeCompare([]byte(reqHashHex), []byte(storedHash)) == 1
}

// GenerateRandomToken produces cryptographically secure hexadecimal tokens.
func GenerateRandomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// -----------------------------------------------------------------------------
// Rate Limiter & Temporary Lockout
// -----------------------------------------------------------------------------

type attemptRecord struct {
	failCount int
	lockUntil time.Time
}

type authRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attemptRecord
}

var globalRateLimiter = &authRateLimiter{
	attempts: make(map[string]*attemptRecord),
}

func (rl *authRateLimiter) checkLockout(key string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rec, exists := rl.attempts[key]
	if !exists {
		return false, 0
	}
	if time.Now().Before(rec.lockUntil) {
		return true, time.Until(rec.lockUntil)
	}
	return false, 0
}

func (rl *authRateLimiter) recordFailure(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rec, exists := rl.attempts[key]
	if !exists {
		rec = &attemptRecord{}
		rl.attempts[key] = rec
	}
	rec.failCount++
	if rec.failCount >= 5 {
		rec.lockUntil = time.Now().Add(5 * time.Minute)
		rec.failCount = 0
	}
}

func (rl *authRateLimiter) recordSuccess(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.attempts, key)
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// -----------------------------------------------------------------------------
// In-Memory User and Session Store (Dev fallback)
// -----------------------------------------------------------------------------

type userStore struct {
	mu    sync.RWMutex
	users map[string]string // username -> bcrypt password hash
	names map[string]string // username -> Display Name
	roles map[string]string // username -> Role ("admin" / "user")
}

var globalUsers = &userStore{
	users: make(map[string]string),
	names: make(map[string]string),
	roles: make(map[string]string),
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
	role := u.roles[oldUsername]
	if newUsername != oldUsername {
		delete(u.users, oldUsername)
		delete(u.names, oldUsername)
		delete(u.roles, oldUsername)
	}
	u.users[newUsername] = passHash
	u.names[newUsername] = newName
	u.roles[newUsername] = role
}

type sessionData struct {
	user      User
	expiresAt time.Time
}

type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]sessionData // hashedToken -> sessionData
}

var globalSessions = &sessionStore{
	sessions: make(map[string]sessionData),
}

func (s *sessionStore) create(user User) (string, error) {
	rawToken, err := GenerateRandomToken(32)
	if err != nil {
		return "", err
	}
	hashedToken := db.HashToken(rawToken)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[hashedToken] = sessionData{
		user:      user,
		expiresAt: time.Now().Add(sessionDuration),
	}
	return rawToken, nil
}

func (s *sessionStore) get(rawToken string) (User, bool) {
	hashedToken := db.HashToken(rawToken)

	s.mu.RLock()
	defer s.mu.RUnlock()

	data, exists := s.sessions[hashedToken]
	if !exists {
		return User{}, false
	}
	if time.Now().After(data.expiresAt) {
		return User{}, false
	}
	return data.user, true
}

func (s *sessionStore) destroy(rawToken string) {
	hashedToken := db.HashToken(rawToken)

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, hashedToken)
}

func (s *sessionStore) update(rawToken string, user User) {
	hashedToken := db.HashToken(rawToken)

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, exists := s.sessions[hashedToken]; exists {
		existing.user = user
		s.sessions[hashedToken] = existing
	}
}

// BootstrapAdmin initializes the primary administrator account from environment variables
// on first run if no admin account exists.
func BootstrapAdmin(database *db.DBWrapper, cfg *config.Config) {
	if cfg == nil {
		return
	}
	adminUser := cfg.AdminBootstrapUser
	if adminUser == "" {
		adminUser = "admin"
	}
	adminPass := cfg.AdminBootstrapPassword
	if adminPass == "" {
		return
	}

	hash, err := HashPassword(adminPass)
	if err != nil {
		log.Printf("[Auth] Notice: Bootstrap password not hashed: %v\n", err)
		return
	}

	// 1. Check & Insert in MySQL if database is connected
	if database != nil && database.Active {
		existing, err := database.GetUserByUsername(adminUser)
		if err == nil && existing != nil {
			log.Printf("[Auth] Admin account %q already exists in database.\n", adminUser)
			return
		}

		userRec := &db.UserRecord{
			Username:     adminUser,
			PasswordHash: hash,
			FullName:     "System Administrator",
			Role:         "admin",
		}
		if err := database.CreateUser(userRec); err == nil {
			log.Printf("[Auth] Successfully bootstrapped admin account %q in MySQL.\n", adminUser)
		}
	}

	// 2. Register in fallback in-memory store
	globalUsers.mu.Lock()
	globalUsers.users[adminUser] = hash
	globalUsers.names[adminUser] = "System Administrator"
	globalUsers.roles[adminUser] = "admin"
	globalUsers.mu.Unlock()
}

// -----------------------------------------------------------------------------
// HTTP Handlers & Payloads
// -----------------------------------------------------------------------------

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
	Authenticated bool   `json:"authenticated"`
	User          *User  `json:"user,omitempty"`
	CSRFToken     string `json:"csrfToken,omitempty"`
}

func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// setAuthCookies issues both session and CSRF cookies with strict flags.
func setAuthCookies(w http.ResponseWriter, r *http.Request, rawToken, csrfToken string) {
	secure := isSecureRequest(r)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   int(sessionDuration.Seconds()),
	})

	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    csrfToken,
		Path:     "/",
		HttpOnly: false, // Read by frontend JS to attach to X-CSRF-Token header
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   int(sessionDuration.Seconds()),
	})
}

// LoginHandler handles POST /api/login with rate limiting, session rotation, and bcrypt validation.
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

	clientKey := getClientIP(r) + ":" + req.Username
	if locked, remaining := globalRateLimiter.checkLockout(clientKey); locked {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(LoginResponse{
			Success: false,
			Message: fmt.Sprintf("Too many failed login attempts. Temporarily locked out for %v.", remaining.Round(time.Second)),
		})
		return
	}

	var user User
	var authenticated bool

	// 1. Authenticate against MySQL if active
	if a != nil && a.DB != nil && a.DB.Active {
		dbUser, err := a.DB.GetUserByUsername(req.Username)
		if err == nil && dbUser != nil {
			if CheckPassword(dbUser.PasswordHash, req.Password) {
				user = User{
					ID:       dbUser.ID,
					Username: dbUser.Username,
					Name:     dbUser.FullName,
					Role:     dbUser.Role,
				}
				authenticated = true
			} else {
				globalRateLimiter.recordFailure(clientKey)
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(LoginResponse{
					Success: false,
					Message: "Invalid username or password",
				})
				return
			}
		}
	}

	// 2. Fallback to in-memory store if DB is offline
	if !authenticated {
		globalUsers.mu.RLock()
		expectedHash, exists := globalUsers.users[req.Username]
		displayName := globalUsers.names[req.Username]
		userRole := globalUsers.roles[req.Username]
		globalUsers.mu.RUnlock()

		if !exists || !CheckPassword(expectedHash, req.Password) {
			globalRateLimiter.recordFailure(clientKey)
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
		if userRole == "" {
			userRole = "admin"
		}

		user = User{
			Username: req.Username,
			Name:     displayName,
			Role:     userRole,
		}
	}

	// Login successful: reset rate limiter
	globalRateLimiter.recordSuccess(clientKey)

	// Session rotation: invalidate any existing session from request
	if oldCookie, err := r.Cookie(sessionCookieName); err == nil && oldCookie.Value != "" {
		globalSessions.destroy(oldCookie.Value)
		if a != nil && a.DB != nil && a.DB.Active {
			_ = a.DB.DeleteSession(oldCookie.Value)
		}
	}

	// Create new session token and CSRF token
	rawToken, err := globalSessions.create(user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(LoginResponse{
			Success: false,
			Message: "Failed to create session",
		})
		return
	}

	csrfToken, _ := GenerateRandomToken(24)

	// Persist active session in MySQL
	if a != nil && a.DB != nil && a.DB.Active && user.ID != "" {
		_ = a.DB.CreateSession(rawToken, user.ID, time.Now().Add(sessionDuration))
	}

	setAuthCookies(w, r, rawToken, csrfToken)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(LoginResponse{
		Success: true,
		User:    &user,
	})
}

// Standalone LoginHandler
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	defaultApp.LoginHandler(w, r)
}

// RegisterHandler handles POST /api/register. New users strictly receive role "user".
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

	username := strings.TrimSpace(req.Email)
	if username == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Email is required",
		})
		return
	}

	if len(req.Password) < MinPasswordLength {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("Password must be at least %d characters", MinPasswordLength),
		})
		return
	}

	passHash, err := HashPassword(req.Password)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Failed to hash password",
		})
		return
	}

	if req.Name == "" {
		req.Name = username
	}

	// Registered users strictly get role "user"
	newUser := User{
		Username: username,
		Name:     req.Name,
		Role:     "user",
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
			PasswordHash: passHash,
			FullName:     req.Name,
			Role:         "user",
		}
		if err := a.DB.CreateUser(userRec); err == nil {
			newUser.ID = userRec.ID
		}
	}

	// 2. Always register in in-memory store for instant fallback cache
	globalUsers.mu.Lock()
	globalUsers.users[username] = passHash
	globalUsers.names[username] = req.Name
	globalUsers.roles[username] = "user"
	globalUsers.mu.Unlock()

	rawToken, _ := globalSessions.create(newUser)
	csrfToken, _ := GenerateRandomToken(24)

	if a != nil && a.DB != nil && a.DB.Active && newUser.ID != "" {
		_ = a.DB.CreateSession(rawToken, newUser.ID, time.Now().Add(sessionDuration))
	}

	setAuthCookies(w, r, rawToken, csrfToken)

	LogEvent("INFO", fmt.Sprintf("New user registered: %s (%s) with role %s", req.Name, username, newUser.Role))

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"user":    newUser,
	})
}

// Standalone RegisterHandler
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	defaultApp.RegisterHandler(w, r)
}

// SessionHandler handles GET /api/session.
func (a *App) SessionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		json.NewEncoder(w).Encode(SessionResponse{Authenticated: false})
		return
	}

	rawToken := cookie.Value
	user, valid := globalSessions.get(rawToken)

	// Validate against MySQL
	if !valid && a != nil && a.DB != nil && a.DB.Active {
		dbUser, err := a.DB.GetSessionUser(rawToken)
		if err == nil && dbUser != nil {
			user = User{
				ID:       dbUser.ID,
				Username: dbUser.Username,
				Name:     dbUser.FullName,
				Role:     dbUser.Role,
			}
			valid = true
		}
	}

	if !valid {
		json.NewEncoder(w).Encode(SessionResponse{Authenticated: false})
		return
	}

	csrfCookie, _ := r.Cookie(csrfCookieName)
	csrfVal := ""
	if csrfCookie != nil {
		csrfVal = csrfCookie.Value
	}

	json.NewEncoder(w).Encode(SessionResponse{
		Authenticated: true,
		User:          &user,
		CSRFToken:     csrfVal,
	})
}

// Standalone SessionHandler
func SessionHandler(w http.ResponseWriter, r *http.Request) {
	defaultApp.SessionHandler(w, r)
}

// LogoutHandler handles POST /api/logout, invalidating session in DB and clearing cookies.
func (a *App) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		globalSessions.destroy(cookie.Value)
		if a != nil && a.DB != nil && a.DB.Active {
			_ = a.DB.DeleteSession(cookie.Value)
		}
	}

	secure := isSecureRequest(r)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   -1,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   -1,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// Standalone LogoutHandler
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	defaultApp.LogoutHandler(w, r)
}

// -----------------------------------------------------------------------------
// Middleware: Authentication, CSRF & Security Headers
// -----------------------------------------------------------------------------

// RequireAuth middleware protects routes from unauthenticated access.
func (a *App) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Authentication required",
			})
			return
		}

		rawToken := cookie.Value
		user, valid := globalSessions.get(rawToken)

		if !valid && a != nil && a.DB != nil && a.DB.Active {
			dbUser, err := a.DB.GetSessionUser(rawToken)
			if err == nil && dbUser != nil {
				user = User{
					ID:       dbUser.ID,
					Username: dbUser.Username,
					Name:     dbUser.FullName,
					Role:     dbUser.Role,
				}
				valid = true
			}
		}

		if !valid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Session expired or invalid",
			})
			return
		}

		// Attach user to request header for downstream handlers
		r.Header.Set("X-User-Username", user.Username)
		r.Header.Set("X-User-Name", user.Name)
		r.Header.Set("X-User-Role", user.Role)

		next(w, r)
	}
}

// CurrentUser returns the authenticated User for the given request, if present and valid.
func (a *App) CurrentUser(r *http.Request) (*User, bool) {
	if username := r.Header.Get("X-User-Username"); username != "" {
		return &User{
			Username: username,
			Name:     r.Header.Get("X-User-Name"),
			Role:     r.Header.Get("X-User-Role"),
		}, true
	}

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil, false
	}

	user, valid := globalSessions.get(cookie.Value)
	if valid {
		return &user, true
	}

	if a != nil && a.DB != nil && a.DB.Active {
		dbUser, err := a.DB.GetSessionUser(cookie.Value)
		if err == nil && dbUser != nil {
			return &User{
				ID:       dbUser.ID,
				Username: dbUser.Username,
				Name:     dbUser.FullName,
				Role:     dbUser.Role,
			}, true
		}
	}

	return nil, false
}

// RequireAdmin middleware restricts routes to admin users.
func (a *App) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return a.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		role := r.Header.Get("X-User-Role")
		if role != "admin" && role != "Cluster Admin" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Forbidden: administrative privileges required",
			})
			return
		}
		next(w, r)
	})
}

// CSRFMiddleware validates anti-CSRF tokens for state-modifying requests.
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only check state-changing methods
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
			// Exempt unauthenticated onboarding endpoints
			if r.URL.Path == "/api/login" || r.URL.Path == "/api/register" {
				next.ServeHTTP(w, r)
				return
			}

			csrfCookie, err := r.Cookie(csrfCookieName)
			clientToken := r.Header.Get("X-CSRF-Token")
			if clientToken == "" {
				clientToken = r.FormValue("csrf_token")
			}

			if err != nil || csrfCookie.Value == "" || clientToken == "" ||
				subtle.ConstantTimeCompare([]byte(csrfCookie.Value), []byte(clientToken)) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "Forbidden: CSRF token missing or invalid",
				})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// SecurityHeadersMiddleware injects standard defense-in-depth HTTP security headers.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com https://unpkg.com https://api.dicebear.com https://cdnjs.cloudflare.com https://fonts.googleapis.com https://fonts.gstatic.com; img-src 'self' data: https:; frame-ancestors 'none';")

		if isSecureRequest(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		next.ServeHTTP(w, r)
	})
}
