package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
)

// writeFixture creates a file with the given contents and returns its path.
func writeFixture(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestResolveAttachments_None(t *testing.T) {
	atts, err := resolveAttachments(nil, "", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if atts != nil {
		t.Fatalf("want nil, got %v", atts)
	}
}

func TestResolveAttachments_Paths(t *testing.T) {
	a := writeFixture(t, "a.txt", "alpha")
	b := writeFixture(t, "b.log", "bravo")

	atts, err := resolveAttachments([]string{a, b}, "", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(atts) != 2 {
		t.Fatalf("want 2 attachments, got %d", len(atts))
	}
	if atts[0].Name != "a.txt" || string(atts[0].Data) != "alpha" {
		t.Fatalf("first attachment: %q %q", atts[0].Name, atts[0].Data)
	}
	if atts[1].Name != "b.log" || string(atts[1].Data) != "bravo" {
		t.Fatalf("second attachment: %q %q", atts[1].Name, atts[1].Data)
	}
}

func TestResolveAttachments_FilenameOverride(t *testing.T) {
	path := writeFixture(t, "ugly-name-1234.txt", "body")
	atts, err := resolveAttachments([]string{path}, "report.txt", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(atts) != 1 || atts[0].Name != "report.txt" {
		t.Fatalf("want report.txt, got %+v", atts)
	}
}

func TestResolveAttachments_Errors(t *testing.T) {
	dir := t.TempDir()
	empty := writeFixture(t, "empty.txt", "")
	ok := writeFixture(t, "ok.txt", "x")

	many := make([]string, maxAttachments+1)
	for i := range many {
		many[i] = ok
	}

	cases := map[string]struct {
		files    []string
		filename string
		stdin    string
	}{
		"missing path":      {files: []string{filepath.Join(dir, "nope.txt")}},
		"directory":         {files: []string{dir}},
		"empty file":        {files: []string{empty}},
		"too many":          {files: many},
		"stdin no filename": {files: []string{"-"}, stdin: "data"},
		"stdin twice":       {files: []string{"-", "-"}, filename: "x.txt", stdin: "data"},
		"stdin plus a file": {files: []string{"-", ok}, filename: "x.txt", stdin: "data"},
		"stdin empty":       {files: []string{"-"}, filename: "x.txt", stdin: ""},
		"filename with two": {files: []string{ok, ok}, filename: "x.txt"},
		"filename no file":  {filename: "x.txt"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := resolveAttachments(tc.files, tc.filename, strings.NewReader(tc.stdin)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestResolveAttachments_Stdin(t *testing.T) {
	atts, err := resolveAttachments([]string{"-"}, "piped.txt", strings.NewReader("piped bytes"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(atts) != 1 || atts[0].Name != "piped.txt" || string(atts[0].Data) != "piped bytes" {
		t.Fatalf("unexpected attachment: %+v", atts)
	}
}

func TestReadPostInput_ReadConflictsWithStdinFile(t *testing.T) {
	_, _, err := readPostInput("", true, []string{"-"}, "x.txt", strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected --read + --file - to be rejected")
	}
}

func TestReadPostInput_FileWithoutMessage(t *testing.T) {
	path := writeFixture(t, "a.txt", "alpha")
	body, atts, err := readPostInput("", false, []string{path}, "", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if body != "" {
		t.Fatalf("want empty body, got %q", body)
	}
	if len(atts) != 1 {
		t.Fatalf("want 1 attachment, got %d", len(atts))
	}
}

func TestReadPostInput_NothingAtAll(t *testing.T) {
	if _, _, err := readPostInput("", false, nil, "", nil); err == nil {
		t.Fatal("expected an error with neither a message nor a file")
	}
}

// A bad path must fail before the body is considered, so the user sees the
// real problem rather than "provide --message".
func TestReadPostInput_BadPathBeatsMissingMessage(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.txt")
	_, _, err := readPostInput("", false, []string{missing}, "", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "--message") {
		t.Fatalf("want the path error, got %v", err)
	}
}

func TestFileIDs(t *testing.T) {
	got := fileIDs([]*model.FileInfo{{Id: "a"}, nil, {Id: "b"}})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("want [a b], got %v", got)
	}
	if fileIDs(nil) != nil {
		t.Fatal("want nil for no infos")
	}
}

func TestAttachedSuffix(t *testing.T) {
	cases := map[int]string{0: "", 1: " with 1 file", 3: " with 3 files"}
	for n, want := range cases {
		if got := attachedSuffix(n); got != want {
			t.Fatalf("attachedSuffix(%d) = %q, want %q", n, got, want)
		}
	}
}
