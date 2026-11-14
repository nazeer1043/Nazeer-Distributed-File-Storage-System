package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type ProfileUpdateRequest struct {
	Name            string `json:"name"`
	Email           string `json:"email"`
	Organization    string `json:"organization"`
	CurrentPassword string `json:"currentPassword,omitempty"`
	NewPassword     string `json:"newPassword,omitempty"`
}

type UserProfileResponse struct {
	Authenticated bool   `json:"authenticated"`
	User          User   `json:"user"`
	Organization  string `json:"organization"`
	CreatedAt     string `json:"createdAt"`
}

var globalUserOrgs = make(map[string]string)

// ProfileHandler handles GET and POST /api/profile.
func (a *App) ProfileHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Authentication required",
		})
		return
	}

	token := cookie.Value
	sessionUser, valid := globalSessions.get(token)
	if !valid && a != nil && a.DB != nil && a.DB.Active {
		dbUser, err := a.DB.GetSessionUser(token)
		if err == nil && dbUser != nil {
			sessionUser = User{
				ID:       dbUser.ID,
				Username: dbUser.Username,
				Name:     dbUser.FullName,
				Role:     dbUser.Role,
			}
			valid = true
		}
	}

	if !valid {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Session expired or invalid",
		})
		return
	}

	if r.Method == http.MethodGet {
		org := globalUserOrgs[sessionUser.Username]
		if org == "" {
			org = "Nazeer Systems Corp"
		}

		json.NewEncoder(w).Encode(UserProfileResponse{
			Authenticated: true,
			User:          sessionUser,
			Organization:  org,
			CreatedAt:     time.Now().Format("2006-01-02"),
		})
		return
	}

	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		var req ProfileUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Invalid request payload",
			})
			return
		}

		// Handle password change if requested
		if req.NewPassword != "" {
			if len(req.NewPassword) < MinPasswordLength {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": fmt.Sprintf("New password must be at least %d characters", MinPasswordLength),
				})
				return
			}

			// Validate current password
			var passVerified bool
			if a != nil && a.DB != nil && a.DB.Active {
				dbUser, err := a.DB.GetUserByUsername(sessionUser.Username)
				if err == nil && dbUser != nil {
					passVerified = CheckPassword(dbUser.PasswordHash, req.CurrentPassword)
				}
			}

			if !passVerified {
				globalUsers.mu.RLock()
				expectedHash := globalUsers.users[sessionUser.Username]
				globalUsers.mu.RUnlock()
				passVerified = CheckPassword(expectedHash, req.CurrentPassword)
			}

			if !passVerified {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "Current password verification failed",
				})
				return
			}

			newHash, err := HashPassword(req.NewPassword)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "Failed to hash new password",
				})
				return
			}

			// Update in DB & memory
			if a != nil && a.DB != nil && a.DB.Active && sessionUser.ID != "" {
				_ = a.DB.UpdateUserPassword(sessionUser.ID, newHash)
			}
			globalUsers.updatePassword(sessionUser.Username, newHash)
			LogEvent("INFO", fmt.Sprintf("Password changed successfully for user %s", sessionUser.Username))
		}

		// Handle profile details update
		if req.Name != "" {
			sessionUser.Name = req.Name
		}
		newUsername := sessionUser.Username
		if req.Email != "" && req.Email != sessionUser.Username {
			newUsername = req.Email
		}

		if req.Organization != "" {
			globalUserOrgs[newUsername] = req.Organization
		}

		// Update in DB
		if a != nil && a.DB != nil && a.DB.Active && sessionUser.ID != "" {
			_ = a.DB.UpdateUser(sessionUser.ID, sessionUser.Name, newUsername)
		}

		// Update in-memory stores
		globalUsers.updateProfile(sessionUser.Username, newUsername, sessionUser.Name)
		sessionUser.Username = newUsername
		globalSessions.update(token, sessionUser)

		LogEvent("INFO", fmt.Sprintf("User profile updated for %s (%s)", sessionUser.Name, sessionUser.Username))

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Profile updated successfully.",
			"user":    sessionUser,
		})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// Standalone ProfileHandler for backward compatibility
func ProfileHandler(w http.ResponseWriter, r *http.Request) {
	defaultApp.ProfileHandler(w, r)
}

type UserSummary struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	FullName  string `json:"fullName"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
}

// UsersListHandler returns list of all registered users (for admin views).
func (a *App) UsersListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var list []UserSummary

	if a != nil && a.DB != nil && a.DB.Active {
		dbUsers, err := a.DB.GetAllUsers()
		if err == nil {
			for _, u := range dbUsers {
				list = append(list, UserSummary{
					ID:        u.ID,
					Username:  u.Username,
					FullName:  u.FullName,
					Role:      u.Role,
					CreatedAt: u.CreatedAt.Format("2006-01-02 15:04"),
				})
			}
		}
	}

	if len(list) == 0 {
		globalUsers.mu.RLock()
		for uname := range globalUsers.users {
			role := globalUsers.roles[uname]
			if role == "" {
				role = "user"
			}
			name := globalUsers.names[uname]
			if name == "" {
				name = uname
			}
			list = append(list, UserSummary{
				ID:        "usr-" + uname,
				Username:  uname,
				FullName:  name,
				Role:      role,
				CreatedAt: "2026-11-01 10:00",
			})
		}
		globalUsers.mu.RUnlock()
	}

	json.NewEncoder(w).Encode(list)
}

// Standalone UsersListHandler for backward compatibility
func UsersListHandler(w http.ResponseWriter, r *http.Request) {
	defaultApp.UsersListHandler(w, r)
}
