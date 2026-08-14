package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/client"
)

// maxAttachments mirrors the per-post limit the Mattermost webapp enforces.
const maxAttachments = 10

// attachment is a resolved --file argument: the bytes plus the name the server
// should store them under.
type attachment struct {
	Name string
	Data []byte
}

// addAttachmentFlags registers the shared --file/--filename pair on the
// commands that create a post.
func addAttachmentFlags(cmd *cobra.Command, files *[]string, filename *string) {
	cmd.Flags().StringArrayVarP(files, "file", "f", nil,
		`Attach a file (repeatable, max 10; "-" reads stdin). Uploaded before the post is created; if any upload fails, no post is made.`)
	cmd.Flags().StringVar(filename, "filename", "",
		`Name to store the attachment under (required with --file -)`)
}

// readPostInput arbitrates the message body and the attachments for commands
// that create a post. Both --read and --file - want stdin, so this is the one
// place that decides who gets it.
func readPostInput(message string, read bool, files []string, filename string, stdin io.Reader) (string, []attachment, error) {
	if read {
		for _, f := range files {
			if f == "-" {
				return "", nil, errors.New(`--read and --file - both consume stdin; use one or the other`)
			}
		}
	}
	atts, err := resolveAttachments(files, filename, stdin)
	if err != nil {
		return "", nil, err
	}
	body, err := readMessageInput(message, read, stdin, len(atts) > 0)
	if err != nil {
		return "", nil, err
	}
	return body, atts, nil
}

// resolveAttachments reads every --file up front so a bad path fails before any
// byte reaches the network. "-" reads stdin and requires --filename.
func resolveAttachments(files []string, filename string, stdin io.Reader) ([]attachment, error) {
	if len(files) == 0 {
		if filename != "" {
			return nil, errors.New("--filename requires --file")
		}
		return nil, nil
	}
	if len(files) > maxAttachments {
		return nil, fmt.Errorf("too many attachments: %d (max %d per post)", len(files), maxAttachments)
	}
	// stdin can only be read once and only under a name --filename supplies,
	// so it is always the sole attachment.
	for _, f := range files {
		if f != "-" {
			continue
		}
		if len(files) > 1 {
			return nil, errors.New("--file - must be the only attachment")
		}
		if filename == "" {
			return nil, errors.New("--file - requires --filename to name the attachment")
		}
	}
	if filename != "" && len(files) > 1 {
		return nil, errors.New("--filename names a single attachment; drop it when passing several --file")
	}

	atts := make([]attachment, 0, len(files))
	for _, path := range files {
		if path == "-" {
			if stdin == nil {
				stdin = os.Stdin
			}
			data, err := io.ReadAll(stdin)
			if err != nil {
				return nil, fmt.Errorf("read stdin: %w", err)
			}
			if len(data) == 0 {
				return nil, errors.New("stdin was empty; nothing to attach")
			}
			atts = append(atts, attachment{Name: filename, Data: data})
			continue
		}

		fi, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("attachment %s: %w", path, err)
		}
		if fi.IsDir() {
			return nil, fmt.Errorf("attachment %s is a directory", path)
		}
		if fi.Size() == 0 {
			return nil, fmt.Errorf("attachment %s is empty", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		name := filename
		if name == "" {
			name = filepath.Base(path)
		}
		atts = append(atts, attachment{Name: name, Data: data})
	}
	return atts, nil
}

// uploadAttachments uploads each attachment to channelID, in order, and returns
// the resulting file infos. On failure the caller must not create the post;
// already-uploaded files stay unattached and the server reaps them.
func uploadAttachments(ctx context.Context, c *model.Client4, channelID string, atts []attachment) ([]*model.FileInfo, error) {
	if len(atts) == 0 {
		return nil, nil
	}
	defer client.WithTimeout(c, client.UploadTimeout)()

	infos := make([]*model.FileInfo, 0, len(atts))
	for _, a := range atts {
		var resp *model.FileUploadResponse
		// The bytes are already buffered, so a retry is cheap to attempt.
		// client.Retry gives up after 45s elapsed, so in practice only quick
		// uploads are retried — a slow one gets a single attempt.
		if _, err := client.Retry(ctx, func() (*model.Response, error) {
			var r *model.Response
			var err error
			resp, r, err = c.UploadFile(ctx, a.Data, channelID, a.Name)
			return r, err
		}); err != nil {
			return nil, fmt.Errorf("upload %s: %w", a.Name, classifyOrWrap(err))
		}
		if resp == nil || len(resp.FileInfos) == 0 {
			return nil, fmt.Errorf("upload %s: server returned no file info", a.Name)
		}
		infos = append(infos, resp.FileInfos[0])
	}
	return infos, nil
}

// fileIDs extracts the ids to put in Post.FileIds.
func fileIDs(infos []*model.FileInfo) []string {
	if len(infos) == 0 {
		return nil
	}
	ids := make([]string, 0, len(infos))
	for _, f := range infos {
		if f != nil {
			ids = append(ids, f.Id)
		}
	}
	return ids
}

// attachedSuffix is the trailing clause human output appends when a post
// carries attachments.
func attachedSuffix(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return " with 1 file"
	default:
		return fmt.Sprintf(" with %d files", n)
	}
}
