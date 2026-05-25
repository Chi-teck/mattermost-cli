package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ayusavin/mattermost-cli/internal/client"
	"github.com/ayusavin/mattermost-cli/internal/config"
	"github.com/ayusavin/mattermost-cli/internal/errs"
)

func init() {
	Register(newLoginCmd)
	Register(newLogoutCmd)
}

func newLoginCmd() *cobra.Command {
	var (
		urlFlag   string
		tokenFlag string
		login     string
		mfa       string
		readToken bool
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate against a Mattermost server",
		Long: "Authenticate against a Mattermost server and persist credentials.\n" +
			"\n" +
			"Preferred path is a Personal Access Token. Use --read-token to read\n" +
			"the token from stdin instead of passing it on the command line.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			if urlFlag == "" {
				return errs.Errorf(errs.CodeGeneric, "--url is required")
			}
			token := strings.TrimSpace(tokenFlag)
			if readToken {
				b, err := io_ReadAll(os.Stdin)
				if err != nil {
					return errs.Errorf(errs.CodeGeneric, "read token from stdin: %s", err.Error())
				}
				token = strings.TrimSpace(string(b))
			}
			if token == "" && login != "" {
				password, err := readPassword("Password: ")
				if err != nil {
					return errs.Errorf(errs.CodeGeneric, "%s", err.Error())
				}
				c, err := client.New(urlFlag, "")
				if err != nil {
					return errs.Errorf(errs.CodeGeneric, "%s", err.Error())
				}
				var u any
				if mfa != "" {
					_, _, err = c.LoginWithMFA(ctx, login, password, mfa)
				} else {
					_, _, err = c.Login(ctx, login, password)
				}
				if err != nil {
					return errs.Errorf(errs.CodeAuthExpired, "login failed: %s", err.Error())
				}
				token = c.AuthToken
				_ = u
			}
			if token == "" {
				return errs.Errorf(errs.CodeGeneric,
					"either --token, --read-token, or --login must be supplied")
			}
			c, err := client.New(urlFlag, token)
			if err != nil {
				return errs.Errorf(errs.CodeGeneric, "%s", err.Error())
			}
			me, err := client.Login(ctx, c)
			if err != nil {
				return err
			}
			cfg := config.Config{
				URL:        urlFlag,
				Token:      token,
				AuthMethod: authMethodForFlags(tokenFlag, login, readToken),
			}
			if _, err := config.Save(cfg); err != nil {
				return errs.Errorf(errs.CodeGeneric, "save config: %s", err.Error())
			}
			if Globals.Human {
				fmt.Fprintf(os.Stdout, "Logged in as %s @ %s\n", me.Username, urlFlag)
				return nil
			}
			return writeJSON(os.Stdout, map[string]any{
				"status":   "ok",
				"username": me.Username,
				"user_id":  me.Id,
				"url":      urlFlag,
			})
		},
	}
	cmd.Flags().StringVar(&urlFlag, "url", os.Getenv("MATTERMOST_URL"), "Server URL (e.g. https://chat.example.com)")
	cmd.Flags().StringVar(&tokenFlag, "token", "", "Personal access token")
	cmd.Flags().BoolVar(&readToken, "read-token", false, "Read token from stdin instead of --token")
	cmd.Flags().StringVar(&login, "login", "", "Username or email for password-based login")
	cmd.Flags().StringVar(&mfa, "mfa", "", "MFA token (with --login)")
	return cmd
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Invalidate the session and clear local credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			cfg, err := config.Resolve()
			if err == nil {
				if c, cerr := client.New(cfg.URL, cfg.Token); cerr == nil {
					// best-effort server-side logout; ignore error
					_ = client.LogoutBestEffort(ctx, c)
				}
			}
			if err := config.Clear(); err != nil {
				return errs.Errorf(errs.CodeGeneric, "clear config: %s", err.Error())
			}
			if Globals.Human {
				fmt.Fprintln(os.Stdout, "Logged out.")
				return nil
			}
			return writeJSON(os.Stdout, map[string]any{"status": "ok"})
		},
	}
}

func authMethodForFlags(token, login string, readStdin bool) string {
	switch {
	case readStdin:
		return "token_stdin"
	case token != "":
		return "token"
	case login != "":
		return "password"
	default:
		return "token"
	}
}

func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("stdin is not a terminal; pass --token or --read-token instead")
	}
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
