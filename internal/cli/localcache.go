package cli

import (
	"context"
	"database/sql"
	"os"

	"github.com/ayusavin/mattermost-cli/internal/errs"
	"github.com/ayusavin/mattermost-cli/internal/store"
	"github.com/ayusavin/mattermost-cli/internal/syncd"
)

// openFreshLocalCache opens the local SQLite cache read-only when a healthy sync
// daemon is keeping it fresh (running + initial backfill complete). It returns
// ok=false to tell read commands to fall back to the live API, so behaviour is
// never worse than today. MM_NO_DAEMON forces the live path.
func openFreshLocalCache(ctx context.Context) (*sql.DB, bool) {
	if os.Getenv("MM_NO_DAEMON") != "" {
		return nil, false
	}
	s, exists, err := syncd.ReadState(ctx)
	if err != nil || !exists || !syncd.Running(s) || !s.BackfillDone {
		return nil, false
	}
	path, err := store.DBPath(false)
	if err != nil {
		return nil, false
	}
	db, err := store.Open(path, true)
	if err != nil {
		return nil, false
	}
	return db, true
}

// localTeamID resolves a team reference (id/name/display name) to a team id
// using the local cache. An empty ref means "all teams" and returns "".
func localTeamID(ctx context.Context, db *sql.DB, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	var id string
	err := db.QueryRowContext(ctx,
		`SELECT id FROM teams WHERE id=? OR name=? OR display_name=? LIMIT 1`,
		ref, ref, ref).Scan(&id)
	if err == sql.ErrNoRows {
		return "", errs.Errorf(errs.CodeGeneric, "team %q not found", ref)
	}
	if err != nil {
		return "", err
	}
	return id, nil
}
