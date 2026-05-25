package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	Register(newDownloadCmd)
}

func newDownloadCmd() *cobra.Command {
	var (
		output string
		force  bool
	)
	cmd := &cobra.Command{
		Use:   "download <file-id>",
		Short: "Download an attached file by its file ID",
		Long: `Download a Mattermost file attachment to disk (or stdout).

File IDs come from the "files[].id" field of any post returned by
mm messages / mm thread / mm mentions / mm search.

By default the file is written to the current directory using its original
name. Pass --output PATH to override, or --output - to stream raw bytes to
stdout.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runDownload(ctx, args[0], output, force)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output path (\"-\" for stdout); defaults to the original filename in cwd")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing file")
	return cmd
}

func runDownload(ctx context.Context, fileID, output string, force bool) error {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return fmt.Errorf("file id is required")
	}

	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}

	info, _, err := c.Client.GetFileInfo(ctx, fileID)
	if err != nil {
		return classifyOrWrap(err)
	}

	data, _, err := c.Client.DownloadFile(ctx, fileID, true)
	if err != nil {
		return classifyOrWrap(err)
	}

	if output == "-" {
		if _, err := os.Stdout.Write(data); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		return nil
	}

	path := output
	if path == "" {
		name := strings.TrimSpace(info.Name)
		if name == "" {
			name = fileID
			if info.Extension != "" {
				name += "." + info.Extension
			}
		}
		path = filepath.Join(".", filepath.Base(name))
	} else if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		name := strings.TrimSpace(info.Name)
		if name == "" {
			name = fileID
		}
		path = filepath.Join(path, filepath.Base(name))
	}

	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; pass --force to overwrite or --output to choose a different path", path)
		}
	}

	if err := writeFileAtomic(path, data); err != nil {
		return err
	}

	if Globals.Human {
		fmt.Fprintf(os.Stdout, "Saved %s (%d bytes) to %s\n", info.Name, len(data), path)
		return nil
	}
	return writeJSON(os.Stdout, map[string]any{
		"id":        info.Id,
		"name":      info.Name,
		"size":      info.Size,
		"mime_type": info.MimeType,
		"extension": info.Extension,
		"width":     info.Width,
		"height":    info.Height,
		"path":      path,
	})
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".mm-download-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := io.Copy(tmp, strings.NewReader(string(data))); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename to %s: %w", path, err)
	}
	return nil
}
