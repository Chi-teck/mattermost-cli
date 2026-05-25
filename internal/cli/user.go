package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/format"
	"github.com/ayusavin/mattermost-cli/internal/resolve"
)

func init() {
	Register(newUserCmd)
}

func newUserCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "user <id-or-username>",
		Short: "Show details about a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			lc, err := LoadContext(ctx)
			if err != nil {
				return err
			}
			r := resolve.New(lc.Client, lc.Me.Id)
			u, err := r.ResolveUser(ctx, args[0])
			if err != nil {
				return classifyOrWrap(err)
			}
			status, _, err := lc.Client.GetUserStatus(ctx, u.Id, "")
			if err != nil {
				return classifyOrWrap(err)
			}

			out := userJSONMap(u, status)
			if Globals.Human {
				fmt.Fprint(os.Stdout, userMarkdown(out))
				return nil
			}
			return writeJSON(os.Stdout, out)
		},
	}
}

func userJSONMap(u *model.User, status *model.Status) map[string]any {
	state := ""
	lastActivityAt := ""
	if status != nil {
		state = status.Status
		lastActivityAt = format.ISOms(status.LastActivityAt)
	}
	return map[string]any{
		"user_id":    u.Id,
		"username":   u.Username,
		"email":      u.Email,
		"first_name": u.FirstName,
		"last_name":  u.LastName,
		"nickname":   u.Nickname,
		"position":   u.Position,
		"status": map[string]any{
			"state":            state,
			"last_activity_at": lastActivityAt,
		},
		"roles":      u.Roles,
		"locale":     u.Locale,
		"timezone":   model.GetPreferredTimezone(u.Timezone),
		"created_at": format.ISOms(u.CreateAt),
		"update_at":  format.ISOms(u.UpdateAt),
	}
}

func userMarkdown(info map[string]any) string {
	status, _ := info["status"].(map[string]any)
	return fmt.Sprintf("User: @%s\nID: %s\nEmail: %s\nFirst name: %s\nLast name: %s\nNickname: %s\nPosition: %s\nStatus: %s\nLast activity: %s\nRoles: %s\nLocale: %s\nTimezone: %s\nCreated: %s\nUpdated: %s\n",
		info["username"], info["user_id"], info["email"], info["first_name"], info["last_name"], info["nickname"], info["position"],
		status["state"], status["last_activity_at"], info["roles"], info["locale"], info["timezone"], info["created_at"], info["update_at"])
}
