package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ayusavin/mattermost-cli/internal/format"
)

func openTempRW(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"), false)
	if err != nil {
		t.Fatalf("open rw: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func TestMigrateCreatesSchema(t *testing.T) {
	db := openTempRW(t)
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if v != SchemaVersion {
		t.Fatalf("user_version = %d, want %d", v, SchemaVersion)
	}
	// Every declared table/view must exist.
	for _, name := range []string{
		"account", "teams", "channels", "channel_members", "users", "statuses",
		"posts", "posts_fts", "reactions", "files", "channel_cursors", "sync_state",
		"v_channel", "v_unread", "v_post", "v_thread",
	} {
		var got string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE name = ?", name,
		).Scan(&got)
		if err != nil {
			t.Fatalf("object %q missing: %v", name, err)
		}
	}
}

func TestMigrateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idem.db")
	db1, err := Open(path, false)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_ = db1.Close()

	db2, err := Open(path, false) // re-open re-runs Migrate; must be a no-op
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer db2.Close()
	var v int
	if err := db2.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if v != SchemaVersion {
		t.Fatalf("user_version after re-open = %d, want %d", v, SchemaVersion)
	}
}

func TestVUnreadMath(t *testing.T) {
	db := openTempRW(t)
	mustExec(t, db, `INSERT INTO teams(id, display_name) VALUES ('t1','Eng')`)
	// unread channel: 10 total, read 7 -> 3 unread
	mustExec(t, db, `INSERT INTO channels(id, team_id, name, display_name, type, total_msg_count, last_post_at)
		VALUES ('c1','t1','prs','PRS','O',10,1700000000000)`)
	mustExec(t, db, `INSERT INTO channel_members(channel_id, user_id, msg_count, mention_count)
		VALUES ('c1','me',7,2)`)
	// fully-read channel: must not appear
	mustExec(t, db, `INSERT INTO channels(id, team_id, name, display_name, type, total_msg_count)
		VALUES ('c2','t1','read','Read','O',5)`)
	mustExec(t, db, `INSERT INTO channel_members(channel_id, user_id, msg_count) VALUES ('c2','me',5)`)

	rows, err := db.Query(`SELECT id, team, unread_count, mention_count, last_post_at_iso FROM v_unread`)
	if err != nil {
		t.Fatalf("query v_unread: %v", err)
	}
	defer rows.Close()
	var (
		id, team, iso   string
		unread, mention int64
		count           int
	)
	for rows.Next() {
		if err := rows.Scan(&id, &team, &unread, &mention, &iso); err != nil {
			t.Fatalf("scan: %v", err)
		}
		count++
	}
	if count != 1 {
		t.Fatalf("v_unread rows = %d, want 1", count)
	}
	if id != "c1" || team != "Eng" || unread != 3 || mention != 2 {
		t.Fatalf("v_unread = {id:%s team:%s unread:%d mention:%d}, want {c1 Eng 3 2}", id, team, unread, mention)
	}
	if want := format.TimestampMS(1700000000000); iso != want {
		t.Fatalf("last_post_at_iso = %q, want %q", iso, want)
	}
}

func TestVPostEnrichment(t *testing.T) {
	db := openTempRW(t)
	mustExec(t, db, `INSERT INTO teams(id, display_name) VALUES ('t1','Eng')`)
	mustExec(t, db, `INSERT INTO channels(id, team_id, name, display_name, type) VALUES ('c1','t1','prs','PRS','O')`)
	mustExec(t, db, `INSERT INTO users(id, username) VALUES ('u1','alice')`)
	const createMs = int64(1700000000000)
	mustExec(t, db, `INSERT INTO posts(id, channel_id, user_id, root_id, message, create_at, reply_count, file_ids_json)
		VALUES ('p1','c1','u1','','hello deploy',?,4,'["f1","f2"]')`, createMs)
	mustExec(t, db, `INSERT INTO posts(id, channel_id, user_id, root_id, message, create_at)
		VALUES ('p2','c1','u1','p1','a reply',?)`, createMs+1000)
	mustExec(t, db, `INSERT INTO reactions(post_id, user_id, emoji_name) VALUES ('p1','u2','thumbsup')`)
	mustExec(t, db, `INSERT INTO reactions(post_id, user_id, emoji_name) VALUES ('p1','u3','thumbsup')`)
	mustExec(t, db, `INSERT INTO files(id, post_id, name, mime_type) VALUES ('f1','p1','a.png','image/png')`)

	// Root post.
	var (
		threadID, author, createdAt, reactions string
		isReply, fileCount, isBot              int
		replyCount                             int64
	)
	err := db.QueryRow(`SELECT thread_id, is_reply, author, created_at, file_count, reply_count, is_bot, reactions
		FROM v_post WHERE id='p1'`).Scan(
		&threadID, &isReply, &author, &createdAt, &fileCount, &replyCount, &isBot, &reactions,
	)
	if err != nil {
		t.Fatalf("query root post: %v", err)
	}
	if threadID != "p1" || isReply != 0 || author != "alice" || fileCount != 2 || replyCount != 4 || isBot != 0 {
		t.Fatalf("root v_post = {thread:%s reply:%d author:%s files:%d replies:%d bot:%d}",
			threadID, isReply, author, fileCount, replyCount, isBot)
	}
	if want := format.TimestampMS(createMs); createdAt != want {
		t.Fatalf("created_at = %q, want %q", createdAt, want)
	}
	if reactions != `{"thumbsup":2}` {
		t.Fatalf("reactions = %q, want {\"thumbsup\":2}", reactions)
	}

	// Reply post: thread_id points at root, is_reply set, reply_count cleared.
	var rThread string
	var rIsReply int
	var rReply int64
	if err := db.QueryRow(`SELECT thread_id, is_reply, reply_count FROM v_post WHERE id='p2'`).
		Scan(&rThread, &rIsReply, &rReply); err != nil {
		t.Fatalf("query reply: %v", err)
	}
	if rThread != "p1" || rIsReply != 1 || rReply != 0 {
		t.Fatalf("reply v_post = {thread:%s reply:%d replyCount:%d}, want {p1 1 0}", rThread, rIsReply, rReply)
	}
}

func TestVPostWebhookBot(t *testing.T) {
	db := openTempRW(t)
	mustExec(t, db, `INSERT INTO posts(id, channel_id, user_id, message, props_json)
		VALUES ('p1','c1','u1','from ci', '{"from_webhook":"true","override_username":"CI Bot"}')`)
	var isBot int
	var botName string
	if err := db.QueryRow(`SELECT is_bot, bot_name FROM v_post WHERE id='p1'`).Scan(&isBot, &botName); err != nil {
		t.Fatalf("query: %v", err)
	}
	if isBot != 1 || botName != "CI Bot" {
		t.Fatalf("webhook post = {is_bot:%d bot_name:%q}, want {1 \"CI Bot\"}", isBot, botName)
	}
}

func TestFTSTriggers(t *testing.T) {
	db := openTempRW(t)
	mustExec(t, db, `INSERT INTO posts(id, channel_id, message) VALUES ('p1','c1','deploy finished')`)
	mustExec(t, db, `INSERT INTO posts(id, channel_id, message) VALUES ('p2','c1','lunch plans')`)
	mustExec(t, db, `INSERT INTO posts(id, channel_id, message) VALUES ('p3','c1','rollback deploy now')`)

	match := func() []string {
		rows, err := db.Query(`SELECT p.id FROM posts p JOIN posts_fts f ON p.rowid = f.rowid
			WHERE posts_fts MATCH 'deploy' ORDER BY p.id`)
		if err != nil {
			t.Fatalf("match: %v", err)
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scan: %v", err)
			}
			ids = append(ids, id)
		}
		return ids
	}

	if got := match(); len(got) != 2 || got[0] != "p1" || got[1] != "p3" {
		t.Fatalf("initial match = %v, want [p1 p3]", got)
	}

	// Update trigger: editing p2 to contain the term adds it.
	mustExec(t, db, `UPDATE posts SET message='deploy retry' WHERE id='p2'`)
	if got := match(); len(got) != 3 {
		t.Fatalf("after update match = %v, want 3 rows", got)
	}

	// Delete trigger: removing p1 drops it from the index.
	mustExec(t, db, `DELETE FROM posts WHERE id='p1'`)
	got := match()
	for _, id := range got {
		if id == "p1" {
			t.Fatalf("deleted post still in FTS index: %v", got)
		}
	}
}

func TestOpenReadOnlyMissing(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "absent.db"), true); err == nil {
		t.Fatal("expected error opening missing read-only DB, got nil")
	}
}
