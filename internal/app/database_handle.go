package app

import (
	"database/sql"

	"go.uber.org/fx"

	"9router/proxy/internal/db"
)

// DatabaseHandleModule supplies the already-open *sql.DB to callers that live
// outside the request path.
//
// The CLI launcher runs in the same process as the gateway and draws its menu
// after fx has started, so the shared handle is already open and configured
// (WAL, busy_timeout, foreign keys). Handing that same handle to the persona
// loader keeps one connection to the settings row the gateway reloads per
// request — the menu never opens a second database.
var DatabaseHandleModule = fx.Module("database-handle",
	fx.Provide(ProvideDatabaseHandle),
)

// ProvideDatabaseHandle exposes the live handle without disturbing the
// DatabaseModule lifecycle that opens and closes it.
func ProvideDatabaseHandle(conn *sql.DB) *db.Handle {
	return &db.Handle{DB: conn}
}
