package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/errs"
	"github.com/ayusavin/mattermost-cli/internal/store"
)

const (
	queryRowCap  = 5000
	queryTimeout = 10 * time.Second
)

func init() {
	Register(newQueryCmd)
}

func newQueryCmd() *cobra.Command {
	var schema bool
	cmd := &cobra.Command{
		Use:   "query [SQL]",
		Short: "Run a read-only SQL query against the local cache",
		Long: "Run a read-only SQL query against the local SQLite cache maintained by the\n" +
			"sync daemon. Only SELECT / WITH / EXPLAIN statements are allowed. Query the\n" +
			"enriched views (v_post, v_channel, v_unread, v_thread) or raw tables. Use\n" +
			"--schema to print the full table/view definitions.\n\n" +
			"Examples:\n" +
			"  mm query \"SELECT name, display_name, unread_count FROM v_unread ORDER BY unread_count DESC\"\n" +
			"  mm query \"SELECT author, message, created_at FROM v_post WHERE channel_id='c123' ORDER BY create_at DESC LIMIT 30\"\n" +
			"  mm query \"SELECT * FROM sync_state\"   # freshness\n" +
			"  mm query --schema",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if schema {
				return runQuery(cmdContext(cmd), schemaSQLQuery)
			}
			if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
				return errs.Errorf(errs.CodeGeneric, "provide a SQL query, or use --schema")
			}
			if err := validateReadOnlySQL(args[0]); err != nil {
				return errs.Errorf(errs.CodeGeneric, "%s", err.Error())
			}
			return runQuery(cmdContext(cmd), args[0])
		},
	}
	cmd.Flags().BoolVar(&schema, "schema", false, "Print the cache schema (tables, views, columns) instead of running a query")
	return cmd
}

// schemaSQLQuery returns the DDL of every queryable table and view so agents can
// discover the surface. Internal FTS shadow tables and indexes are hidden.
const schemaSQLQuery = `SELECT type, name, sql FROM sqlite_master ` +
	`WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%' ` +
	`AND name NOT GLOB '*_fts_*' AND sql IS NOT NULL ORDER BY type, name`

// validateReadOnlySQL rejects anything that isn't a single read-only statement.
// The DB is also opened read-only, so this is defense in depth against ATTACH,
// PRAGMA writes, and statement stacking.
func validateReadOnlySQL(query string) error {
	s := strings.TrimSpace(query)
	s = strings.TrimSuffix(strings.TrimSpace(s), ";")
	if strings.Contains(s, ";") {
		return fmt.Errorf("only a single statement is allowed")
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "select") || strings.HasPrefix(low, "with") || strings.HasPrefix(low, "explain") {
		return nil
	}
	return fmt.Errorf("only SELECT / WITH / EXPLAIN queries are allowed (read-only cache)")
}

func runQuery(ctx context.Context, query string) error {
	path, err := store.DBPath(false)
	if err != nil {
		return errs.Errorf(errs.CodeGeneric, "%s", err.Error())
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return errs.Errorf(errs.CodeGeneric,
			"no local cache at %s; run 'mm sync start' first", path)
	}
	db, err := store.Open(path, true)
	if err != nil {
		return errs.Errorf(errs.CodeGeneric, "open cache: %s", err.Error())
	}
	defer db.Close()

	qctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := db.QueryContext(qctx, query)
	if err != nil {
		return errs.Errorf(errs.CodeGeneric, "query: %s", err.Error())
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return errs.Errorf(errs.CodeGeneric, "columns: %s", err.Error())
	}

	out := make([]map[string]any, 0, 64)
	truncated := false
	for rows.Next() {
		if len(out) >= queryRowCap {
			truncated = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return errs.Errorf(errs.CodeGeneric, "scan: %s", err.Error())
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = normalizeSQLValue(vals[i])
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return errs.Errorf(errs.CodeGeneric, "rows: %s", err.Error())
	}
	if truncated {
		fmt.Fprintf(os.Stderr, "warning: result truncated at %d rows\n", queryRowCap)
	}

	if Globals.Human {
		fmt.Fprintln(os.Stdout, humanTable(cols, out))
		return nil
	}
	return writeJSON(os.Stdout, out)
}

// normalizeSQLValue turns driver values into JSON-friendly types ([]byte -> string).
func normalizeSQLValue(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

func humanTable(cols []string, rows []map[string]any) string {
	if len(rows) == 0 {
		return "No rows."
	}
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(cols, "\t"))
	for _, row := range rows {
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = sqlCell(row[c])
		}
		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	_ = w.Flush()
	return strings.TrimRight(b.String(), "\n")
}

func sqlCell(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
