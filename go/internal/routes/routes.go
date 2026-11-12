// Package routes wires every optional route module (import/export, AniList) into the HTTP app, as
// the Node server's registerAllRoutes did.
package routes

import (
	"github.com/reeper316099/Lorekeeper/go/internal/anilist"
	"github.com/reeper316099/Lorekeeper/go/internal/api"
	"github.com/reeper316099/Lorekeeper/go/internal/importexport"
)

// Extend registers all optional routes; use it as api.Options.Extend.
func Extend(rt *api.Router, srv *api.Server) {
	importexport.Register(rt, srv)
	anilist.Register(anilist.RouteOptions{})(rt, srv)
}
