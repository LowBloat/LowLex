// Command lowlex runs the LowLex translation server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/LowBloat/LowLex/internal/config"
	"github.com/LowBloat/LowLex/internal/secure"
	"github.com/LowBloat/LowLex/internal/server"
	"github.com/LowBloat/LowLex/internal/store"
	"github.com/LowBloat/LowLex/web"
)

const usage = `LowLex %s, a light translation management server.

Usage:
  lowlex [serve]                         start the server
  lowlex reset-password <user> [new]     set a password (generated when omitted); the user must change it at next sign-in
  lowlex health                          exit 0 when the local server answers (for container health checks)
  lowlex version                         print the version

Settings come from the environment or a .env file (current directory or next to the binary):
  LOWLEX_HOST            address to listen on            (default 0.0.0.0)
  LOWLEX_PORT            port                            (default %d)
  LOWLEX_DATA_DIR        database and key file folder    (default ./lowlex_data)
  LOWLEX_ADMIN_USER      first admin's username          (default admin)
  LOWLEX_ADMIN_PASSWORD  first admin's password; when empty it is "lowlex" and must be changed at first sign-in
  LOWLEX_PUBLIC_URL      external URL, e.g. https://lex.example.com (turns on secure cookies for https)
  LOWLEX_SECRET          key for encrypting provider credentials (default: generated into the data folder)
  LOWLEX_LOG_REQUESTS    log every request (true/false)
`

func main() {
	log.SetFlags(log.LstdFlags)
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	cfg := config.Load()
	switch cmd {
	case "serve":
		if err := serve(cfg); err != nil {
			log.Fatal(err)
		}
	case "reset-password":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: lowlex reset-password <user> [new-password]")
			os.Exit(2)
		}
		pw := ""
		if len(os.Args) > 3 {
			pw = os.Args[3]
		}
		if err := resetPassword(cfg, os.Args[2], pw); err != nil {
			log.Fatal(err)
		}
	case "health":
		// Used by container health checks, which have no shell or curl.
		resp, err := (&http.Client{Timeout: 3 * time.Second}).Get("http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port)) + "/api/health")
		if err != nil || resp.StatusCode != http.StatusOK {
			fmt.Fprintln(os.Stderr, "unhealthy", err)
			os.Exit(1)
		}
		fmt.Println("ok")
	case "version", "--version", "-v":
		fmt.Println(server.Version)
	case "help", "--help", "-h":
		fmt.Printf(usage, server.Version, config.DefaultPort)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		fmt.Printf(usage, server.Version, config.DefaultPort)
		os.Exit(2)
	}
}

func serve(cfg config.Config) error {
	db, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := server.Bootstrap(db, cfg.AdminUser, cfg.AdminPassword); err != nil {
		return err
	}
	box, err := secure.NewBox(cfg.Secret, cfg.DataDir)
	if err != nil {
		return err
	}
	ui := web.FS()
	if ui == nil {
		log.Print("warning: this binary has no web UI; build it with `bun run build` in web/ before `go build`")
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(db, cfg, box, ui),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	shown := cfg.Host
	if shown == "0.0.0.0" || shown == "::" || shown == "" {
		shown = "localhost"
	}
	log.Printf("LowLex %s listening on http://%s (data: %s)", server.Version, net.JoinHostPort(shown, strconv.Itoa(cfg.Port)), cfg.DataDir)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Print("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}
	return nil
}

func resetPassword(cfg config.Config, username, pw string) error {
	db, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	generated := pw == ""
	if generated {
		pw = secure.Token(12)
	}
	res, err := db.Exec(`UPDATE users SET password_hash = ?, must_change_password = 1, disabled = 0 WHERE username = ?`, secure.HashPassword(pw), username)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no user named %q", username)
	}
	db.Exec(`DELETE FROM sessions WHERE user_id = (SELECT id FROM users WHERE username = ?)`, username)
	if generated {
		fmt.Printf("Temporary password for %s: %s\n", username, pw)
	} else {
		fmt.Printf("Password updated for %s.\n", username)
	}
	fmt.Println("It must be changed at the next sign-in.")
	return nil
}
