package server

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/LowBloat/LowLex/internal/secure"
)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$`)

// listUsers: admins see every field; other users get a directory (for member pickers is admin-only anyway).
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request, u *User) error {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY username`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type userWithProjects struct {
		*User
		Projects int `json:"projects"`
	}
	list := []userWithProjects{}
	for rows.Next() {
		x, err := scanUser(rows)
		if err != nil {
			return err
		}
		if !u.IsAdmin() {
			x = &User{ID: x.ID, Username: x.Username, Name: x.Name, Role: x.Role, Disabled: x.Disabled}
		}
		list = append(list, userWithProjects{User: x})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	counts := map[int64]int{}
	crows, err := s.db.Query(`SELECT user_id, COUNT(*) FROM project_members GROUP BY user_id`)
	if err != nil {
		return err
	}
	for crows.Next() {
		var id int64
		var n int
		crows.Scan(&id, &n)
		counts[id] = n
	}
	crows.Close()
	for i := range list {
		list[i].Projects = counts[list[i].ID]
	}
	writeJSON(w, 200, map[string]any{"users": list})
	return nil
}

type userInput struct {
	Username           *string `json:"username"`
	Name               *string `json:"name"`
	Email              *string `json:"email"`
	Role               *string `json:"role"`
	Password           *string `json:"password"`
	MustChangePassword *bool   `json:"must_change_password"`
	Disabled           *bool   `json:"disabled"`
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request, admin *User) error {
	var in userInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if in.Username == nil || !usernameRe.MatchString(strings.TrimSpace(*in.Username)) {
		return badRequest("Username: 2 to 64 letters, digits, dot, dash or underscore")
	}
	username := strings.TrimSpace(*in.Username)
	if in.Password == nil {
		return badRequest("Password is required")
	}
	if err := validatePassword(*in.Password, username); err != nil {
		return err
	}
	role := "user"
	if in.Role != nil {
		role = *in.Role
	}
	if role != "admin" && role != "user" {
		return badRequest("Role must be admin or user")
	}
	mustChange := true
	if in.MustChangePassword != nil {
		mustChange = *in.MustChangePassword
	}
	name, email := "", ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.Email != nil {
		email = strings.TrimSpace(*in.Email)
	}
	var exists int
	s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = ?`, username).Scan(&exists)
	if exists > 0 {
		return errf(http.StatusConflict, "username_taken", "This username is already taken")
	}
	res, err := s.db.Exec(`INSERT INTO users (username, name, email, password_hash, role, must_change_password) VALUES (?, ?, ?, ?, ?, ?)`,
		username, name, email, secure.HashPassword(*in.Password), role, mustChange)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	x, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
	if err != nil {
		return err
	}
	s.logActivity(0, admin.ID, "user.create", username)
	writeJSON(w, 201, map[string]any{"user": x})
	return nil
}

func (s *Server) countActiveAdmins(excluding int64) int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled = 0 AND id <> ?`, excluding).Scan(&n)
	return n
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request, admin *User) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return errNotFound
	}
	x, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
	if err != nil {
		return err
	}
	var in userInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if in.Name != nil {
		x.Name = strings.TrimSpace(*in.Name)
	}
	if in.Email != nil {
		x.Email = strings.TrimSpace(*in.Email)
	}
	if in.Role != nil {
		if *in.Role != "admin" && *in.Role != "user" {
			return badRequest("Role must be admin or user")
		}
		if x.Role == "admin" && *in.Role != "admin" && s.countActiveAdmins(x.ID) == 0 {
			return badRequest("Keep at least one active admin")
		}
		x.Role = *in.Role
	}
	if in.Disabled != nil {
		if *in.Disabled && x.ID == admin.ID {
			return badRequest("You cannot disable your own account")
		}
		if *in.Disabled && x.Role == "admin" && s.countActiveAdmins(x.ID) == 0 {
			return badRequest("Keep at least one active admin")
		}
		x.Disabled = *in.Disabled
	}
	if in.MustChangePassword != nil {
		x.MustChangePassword = *in.MustChangePassword
	}
	if in.Password != nil && *in.Password != "" {
		if err := validatePassword(*in.Password, x.Username); err != nil {
			return err
		}
		x.passwordHash = secure.HashPassword(*in.Password)
		if in.MustChangePassword == nil && x.ID != admin.ID {
			x.MustChangePassword = true
		}
		s.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, x.ID)
	}
	if _, err := s.db.Exec(`UPDATE users SET name = ?, email = ?, role = ?, disabled = ?, must_change_password = ?, password_hash = ? WHERE id = ?`,
		x.Name, x.Email, x.Role, x.Disabled, x.MustChangePassword, x.passwordHash, x.ID); err != nil {
		return err
	}
	if x.Disabled {
		s.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, x.ID)
	}
	writeJSON(w, 200, map[string]any{"user": x})
	return nil
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, admin *User) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return errNotFound
	}
	if id == admin.ID {
		return badRequest("You cannot delete your own account")
	}
	x, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
	if err != nil {
		return err
	}
	if x.Role == "admin" && s.countActiveAdmins(x.ID) == 0 {
		return badRequest("Keep at least one active admin")
	}
	if _, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id); err != nil {
		return err
	}
	s.logActivity(0, admin.ID, "user.delete", x.Username)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

// logActivity records an event; projectID 0 means a global event.
func (s *Server) logActivity(projectID, userID int64, action, detail string) {
	var pid, uid any
	if projectID > 0 {
		pid = projectID
	}
	if userID > 0 {
		uid = userID
	}
	s.db.Exec(`INSERT INTO activity (project_id, user_id, action, detail) VALUES (?, ?, ?, ?)`, pid, uid, action, truncate(detail, 500))
}
