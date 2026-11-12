package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/api"
	"github.com/reeper316099/Lorekeeper/go/internal/routes"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

// WebviewOrigins are the origins the page can have inside the Wails window (macOS, Windows, Linux).
// The local API allows exactly these for CORS (plus the dev server, when developing).
var WebviewOrigins = []string{"wails://wails", "wails://wails.localhost", "http://wails.localhost", "https://wails.localhost"}

type Options struct {
	AppVersion string
	// UserData and DataDir are resolved when empty: see UserDataDir and ResolveDataDir.
	UserData string
	DataDir  string
	// AllowedOrigins are the extra CORS origins (a dev server).
	AllowedOrigins []string
	// StaticDir serves a built web app from the API server itself (headless runs); the desktop
	// window serves it from its own embedded assets instead.
	StaticDir string
}

// Core is the running local app: the data store, the HTTP API on 127.0.0.1 with a per-launch bearer
// token, and the lock on the data folder.
type Core struct {
	UserData string
	DataDir  string
	BaseURL  string
	Token    string
	Store    *store.Store
	API      *api.Server
	Log      *Logger

	httpSrv *http.Server
	release func()
}

// Start locks the data folder, opens the store and starts the API on a random localhost port.
func Start(o Options) (*Core, error) {
	userData := o.UserData
	if userData == "" {
		var err error
		if userData, err = UserDataDir(shared.AppName); err != nil {
			return nil, err
		}
	}
	dataDir := o.DataDir
	if dataDir == "" {
		dataDir = ResolveDataDir(os.Getenv("LOREKEEPER_DATA_DIR"), ReadBootstrap(userData).DataDir, userData)
	}
	release, err := LockDataDir(dataDir)
	if err != nil {
		if errors.Is(err, ErrLocked) {
			return nil, fmt.Errorf("%w:\n%s", ErrLocked, dataDir)
		}
		return nil, err
	}
	log := NewLogger(dataDir)
	log.Log("info", fmt.Sprintf("starting %s %s", shared.AppName, o.AppVersion))

	st := store.New(dataDir, store.Options{Watch: true, AppVersion: o.AppVersion})
	st.On(func(e store.Event) {
		switch e.Name {
		case store.EventFileError:
			if fe, ok := e.Data.(store.FileError); ok {
				log.Log("warn", fmt.Sprintf("invalid file %s: %s", fe.File, fe.Message))
			}
		case store.EventFileRestored:
			if m, ok := e.Data.(map[string]string); ok {
				log.Log("warn", "restored "+m["file"]+" from backup")
			}
		}
	})
	if err := st.Open(); err != nil {
		log.Log("error", "failed to open data: "+err.Error())
		release()
		return nil, fmt.Errorf("could not open your data folder:\n%w", err)
	}

	tok := make([]byte, 32)
	if _, err := rand.Read(tok); err != nil {
		st.Close()
		release()
		return nil, err
	}
	token := hex.EncodeToString(tok)
	srv := api.New(api.Options{
		Store: st, Token: token, StaticDir: o.StaticDir,
		AllowedOrigins: append(append([]string(nil), WebviewOrigins...), o.AllowedOrigins...),
		Extend:         routes.Extend,
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		st.Close()
		release()
		return nil, err
	}
	c := &Core{
		UserData: userData, DataDir: dataDir, Token: token, Store: st, API: srv, Log: log, release: release,
		BaseURL: "http://" + ln.Addr().String(),
		httpSrv: &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second},
	}
	go c.httpSrv.Serve(ln)
	log.Log("info", "api listening on "+ln.Addr().String())
	return c, nil
}

// Shutdown ends event streams, stops the server, closes the store and releases the lock.
func (c *Core) Shutdown() {
	c.API.Close() // so open event streams do not hold the shutdown up
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.httpSrv.Shutdown(ctx); err != nil {
		c.httpSrv.Close()
	}
	c.Store.Close()
	c.release()
	c.Log.Log("info", "stopped")
}
