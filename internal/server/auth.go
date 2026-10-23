package server

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/LowBloat/LowLex/internal/secure"
)

const (
	sessionCookie = "lowlex_session"
	sessionTTL    = 30 * 24 * time.Hour
	// DefaultAdminPassword is used for the first admin when LOWLEX_ADMIN_PASSWORD is empty;
	// that admin must replace it on first sign-in.
	DefaultAdminPassword = "lowlex"
	minPasswordLength    = 10
)

type User struct {
	ID                 int64   `json:"id"`
	Username           string  `json:"username"`
	Email              string  `json:"email"`
	Name               string  `json:"name"`
	Role               string  `json:"role"`
	MustChangePassword bool    `json:"must_change_password"`
	Disabled           bool    `json:"disabled"`
	CreatedAt          string  `json:"created_at"`
	LastLoginAt        *string `json:"last_login_at"`
	passwordHash       string
}

func (u *User) IsAdmin() bool { return u != nil && u.Role == "admin" }

const userCols = `id, username, email, name, role, must_change_password, disabled, created_at, last_login_at, password_hash`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.Name, &u.Role, &u.MustChangePassword, &u.Disabled,
		&u.CreatedAt, &u.LastLoginAt, &u.passwordHash)
	return u, err
}

// principal is who is calling: a signed-in user, or an API key bound to one project.
type principal struct {
	user   *User
	apiKey *apiKeyPrincipal
}

type apiKeyPrincipal struct {
	id        int64
	projectID int64
	scope     string
}

type ctxKey struct{}

func principalFrom(ctx context.Context) *principal {
	p, _ := ctx.Value(ctxKey{}).(*principal)
	return p
}

// Bootstrap creates the first admin when the database has no users.
func Bootstrap(db *sql.DB, username, password string) error {
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	mustChange := password == ""
	if mustChange {
		password = DefaultAdminPassword
	}
	_, err := db.Exec(`INSERT INTO users (username, name, password_hash, role, must_change_password) VALUES (?, 'Administrator', ?, 'admin', ?)`,
		username, secure.HashPassword(password), mustChange)
	if err != nil {
		return err
	}
	if mustChange {
		log.Printf("created admin user %q with the default password %q: you will be asked to change it on first sign-in", username, DefaultAdminPassword)
	} else {
		log.Printf("created admin user %q with the password from LOWLEX_ADMIN_PASSWORD", username)
	}
	return nil
}

// authenticate resolves the session cookie, a bearer session token or an API key.
func (s *Server) authenticate(r *http.Request) (*principal, error) {
	token := ""
	if v := r.Header.Get("X-API-Key"); v != "" {
		token = v
	} else if v := r.Header.Get("Authorization"); strings.HasPrefix(v, "Bearer ") {
		token = strings.TrimSpace(v[7:])
	}
	if strings.HasPrefix(token, "lx_") {
		var k apiKeyPrincipal
		err := s.db.QueryRow(`SELECT id, project_id, scope FROM api_keys WHERE key_hash = ?`, secure.HashToken(token)).
			Scan(&k.id, &k.projectID, &k.scope)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errf(http.StatusUnauthorized, "invalid_api_key", "Invalid API key")
		}
		if err != nil {
			return nil, err
		}
		s.db.Exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ? AND (last_used_at IS NULL OR last_used_at < ?)`,
			now(), k.id, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339))
		return &principal{apiKey: &k}, nil
	}

	fromCookie := false
	if token == "" {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			return nil, errUnauthorized
		}
		token = c.Value
		fromCookie = true
	}
	if fromCookie && r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
		return nil, errf(http.StatusForbidden, "bad_origin", "Cross-origin request rejected")
	}
	row := s.db.QueryRow(`SELECT `+prefixCols("u.", userCols)+` FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, secure.HashToken(token), now())
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if u.Disabled {
		return nil, errf(http.StatusUnauthorized, "disabled", "This account is disabled")
	}
	return &principal{user: u}, nil
}

// sameOrigin rejects cookie-authenticated writes sent from another site.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
		host = strings.TrimSpace(strings.Split(fh, ",")[0])
	}
	return strings.EqualFold(u.Host, host)
}

func prefixCols(prefix, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

const (
	adminOnly    = 1 << iota // only global admins
	allowPending             // reachable while a password change is pending
)

// user wraps handlers that need a signed-in person (not an API key).
func (s *Server) user(h func(w http.ResponseWriter, r *http.Request, u *User) error, flags int) http.Handler {
	return handler(func(w http.ResponseWriter, r *http.Request) error {
		p, err := s.authenticate(r)
		if err != nil {
			return err
		}
		if p.user == nil {
			return errf(http.StatusForbidden, "user_required", "This endpoint needs a signed-in user, not an API key")
		}
		if p.user.MustChangePassword && flags&allowPending == 0 {
			return errf(http.StatusForbidden, "password_change_required", "Change your password to continue")
		}
		if flags&adminOnly != 0 && !p.user.IsAdmin() {
			return errForbidden
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, p)
		return h(w, r.WithContext(ctx), p.user)
	})
}

func setSessionCookie(w http.ResponseWriter, s *Server, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	in.Username = strings.TrimSpace(in.Username)
	ip := clientIP(r)
	if wait := s.limiter.blocked(ip, in.Username); wait > 0 {
		return errf(http.StatusTooManyRequests, "rate_limited", "Too many attempts. Try again in %d seconds.", int(wait.Seconds())+1)
	}
	u, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE username = ? OR (email <> '' AND email = ?)`, in.Username, in.Username))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err != nil || !secure.CheckPassword(u.passwordHash, in.Password) || u.Disabled {
		if err != nil {
			secure.CheckPassword(dummyHash, in.Password) // same timing for unknown users
		}
		s.limiter.fail(ip, in.Username)
		return errf(http.StatusUnauthorized, "invalid_credentials", "Wrong username or password")
	}
	s.limiter.reset(ip, in.Username)

	token := secure.Token(32)
	expires := time.Now().UTC().Add(sessionTTL)
	if _, err := s.db.Exec(`INSERT INTO sessions (token_hash, user_id, expires_at, user_agent) VALUES (?, ?, ?, ?)`,
		secure.HashToken(token), u.ID, expires.Format(time.RFC3339), truncate(r.UserAgent(), 200)); err != nil {
		return err
	}
	s.db.Exec(`UPDATE users SET last_login_at = ? WHERE id = ?`, now(), u.ID)
	s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now())
	setSessionCookie(w, s, token, int(sessionTTL.Seconds()))
	writeJSON(w, 200, map[string]any{"user": u})
	return nil
}

var dummyHash = secure.HashPassword("lowlex-timing-equalizer")

func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, secure.HashToken(c.Value))
	}
	setSessionCookie(w, s, "", -1)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, u *User) error {
	writeJSON(w, 200, map[string]any{"user": u})
	return nil
}

func (s *Server) updateMe(w http.ResponseWriter, r *http.Request, u *User) error {
	var in struct {
		Name  *string `json:"name"`
		Email *string `json:"email"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if in.Name != nil {
		u.Name = strings.TrimSpace(*in.Name)
	}
	if in.Email != nil {
		u.Email = strings.TrimSpace(*in.Email)
	}
	if _, err := s.db.Exec(`UPDATE users SET name = ?, email = ? WHERE id = ?`, u.Name, u.Email, u.ID); err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"user": u})
	return nil
}

func validatePassword(pw, username string) error {
	if len([]rune(pw)) < minPasswordLength {
		return badRequest("Use at least %d characters", minPasswordLength)
	}
	if strings.EqualFold(pw, username) || pw == DefaultAdminPassword {
		return badRequest("Choose a password that is not your username or the default password")
	}
	return nil
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, u *User) error {
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if !secure.CheckPassword(u.passwordHash, in.Current) {
		return errf(http.StatusBadRequest, "wrong_password", "The current password is wrong")
	}
	if in.New == in.Current {
		return badRequest("The new password must be different")
	}
	if err := validatePassword(in.New, u.Username); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE users SET password_hash = ?, must_change_password = 0 WHERE id = ?`, secure.HashPassword(in.New), u.ID); err != nil {
		return err
	}
	// sign out other sessions
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.db.Exec(`DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?`, u.ID, secure.HashToken(c.Value))
	}
	u.MustChangePassword = false
	writeJSON(w, 200, map[string]any{"user": u})
	return nil
}

func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		return strings.TrimSpace(strings.Split(xf, ",")[0])
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// loginLimiter slows down password guessing per IP and per username.
type loginLimiter struct {
	mu   sync.Mutex
	hits map[string]*attempts
}

type attempts struct {
	count int
	until time.Time
	last  time.Time
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{hits: map[string]*attempts{}} }

func (l *loginLimiter) keys(ip, user string) []string {
	return []string{"ip:" + ip, "user:" + strings.ToLower(user)}
}

func (l *loginLimiter) blocked(ip, user string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	var wait time.Duration
	for _, k := range l.keys(ip, user) {
		if a := l.hits[k]; a != nil {
			if d := time.Until(a.until); d > wait {
				wait = d
			}
		}
	}
	return wait
}

func (l *loginLimiter) fail(ip, user string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := time.Now()
	for _, k := range l.keys(ip, user) {
		a := l.hits[k]
		if a == nil || t.Sub(a.last) > 15*time.Minute {
			a = &attempts{}
			l.hits[k] = a
		}
		a.count++
		a.last = t
		if a.count >= 5 {
			// 5 failures: 2s, then doubling up to 5 minutes
			d := min(time.Duration(1<<min(a.count-4, 9))*time.Second, 5*time.Minute)
			a.until = t.Add(d)
		}
	}
	if len(l.hits) > 10000 {
		for k, a := range l.hits {
			if t.Sub(a.last) > 15*time.Minute {
				delete(l.hits, k)
			}
		}
	}
}

func (l *loginLimiter) reset(ip, user string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, "user:"+strings.ToLower(user))
}
