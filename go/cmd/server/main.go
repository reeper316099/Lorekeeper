// Command server is the headless Lorekeeper API (dev, tests, e2e): the same HTTP app the desktop
// shell embeds, on 127.0.0.1.
//
//	LOREKEEPER_DATA_DIR=/path LOREKEEPER_TOKEN=secret PORT=4317 go run ./cmd/server
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
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/api"
	"github.com/reeper316099/Lorekeeper/go/internal/routes"
	"github.com/reeper316099/Lorekeeper/go/internal/shared"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	home, _ := os.UserHomeDir()
	dir := env("LOREKEEPER_DATA_DIR", filepath.Join(home, "."+shared.AppSlug+"-dev-go"))
	token := env("LOREKEEPER_TOKEN", "dev-token")
	port, _ := strconv.Atoi(env("PORT", "4317"))

	st := store.New(dir, store.Options{Watch: true})
	if err := st.Open(); err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	srv := api.New(api.Options{
		Store:          st,
		Token:          token,
		AllowedOrigins: []string{"http://localhost:5173", "http://127.0.0.1:5173"},
		StaticDir:      os.Getenv("LOREKEEPER_STATIC"),
		Extend:         routes.Extend,
	})
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		log.Fatal(err)
	}
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	fmt.Printf("data dir: %s\ntoken: %s\nlistening on http://%s\n", dir, token, ln.Addr())

	done := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		srv.Close() // ends open event streams so shutdown does not wait for them
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpSrv.Shutdown(ctx)
		close(done)
	}()
	if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-done
}
