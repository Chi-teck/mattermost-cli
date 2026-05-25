package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/ayusavin/mattermost-cli/internal/client"
	"github.com/ayusavin/mattermost-cli/internal/config"
	"github.com/ayusavin/mattermost-cli/internal/errs"
)

// Context bundles an authenticated client with the user's identity, so
// command handlers don't all repeat the same setup dance.
type Context struct {
	Cfg    config.Config
	Client *model.Client4 // narrowed via client.API at call sites
	Me     *model.User
}

// LoadContext resolves credentials (env > file), builds an SDK client,
// pings the server via GetMe, and returns a ready-to-use Context.
//
// Returns an ExitError with code CodeAuthExpired on 401-style failures
// so callers can exit with the right code without further wrapping.
func LoadContext(ctx context.Context) (*Context, error) {
	cfg, err := config.Resolve()
	if err != nil {
		return nil, errs.Errorf(errs.CodeAuthExpired, "%s", err.Error())
	}
	c, err := client.New(cfg.URL, cfg.Token)
	if err != nil {
		return nil, errs.Errorf(errs.CodeGeneric, "build client: %s", err.Error())
	}
	me, err := client.Login(ctx, c)
	if err != nil {
		return nil, err
	}
	return &Context{Cfg: cfg, Client: c, Me: me}, nil
}

// printJSONOrHuman writes v as JSON unless --human is set, in which case
// humanFunc(v) is printed instead.
func printJSONOrHuman(v any, humanFunc func() string) error {
	if Globals.Human {
		if humanFunc != nil {
			fmt.Fprintln(os.Stdout, humanFunc())
		}
		return nil
	}
	// Lazy: import-free JSON via fmt is wrong; use a tiny helper.
	return writeJSON(os.Stdout, v)
}
