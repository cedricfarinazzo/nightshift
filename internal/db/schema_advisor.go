package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// ColumnDiff describes differences in columns for a table.
type ColumnDiff struct {
	Table          string   `json:"table"`
	MissingColumns []string `json:"missing_columns"`
	ExtraColumns   []string `json:"extra_columns"`
}

// SchemaAnalysis is the conservative, read-only result of analyzing the DB schema
// vs the migration definitions.
type SchemaAnalysis struct {
	MissingMigrations   []Migration  `json:"missing_migrations"`
	UnexpectedObjects   []string     `json:"unexpected_objects"`
	ColumnDiffs         []ColumnDiff `json:"column_diffs"`
	SuggestedMigrations []string     `json:"suggested_migrations"`
}

// AnalyzeSchema performs a conservative comparison between the applied DB schema
// and the migration definitions. It reports missing migrations, objects present
// in the DB but not described by migrations, simple column diffs, and
// suggested SQL snippets (read-only).
func AnalyzeSchema(db *sql.DB) (SchemaAnalysis, error) {
	var res SchemaAnalysis
	if db == nil {
		return res, fmt.Errorf("db is nil")
	}

	currentVersion, err := CurrentVersion(db)
	if err != nil {
		return res, err
	}

	migs := GetMigrations()
	for _, m := range migs {
		if m.Version > currentVersion {
			res.MissingMigrations = append(res.MissingMigrations, m)
			res.SuggestedMigrations = append(res.SuggestedMigrations, m.SQL)
		}
	}

	migrationTables, migrationCols := parseMigrations(migs)

	// Collect DB tables (best-effort) and ignore internal sqlite_* and schema_version.
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		return res, fmt.Errorf("query sqlite_master: %w", err)
	}
	defer rows.Close()
	dbTables := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return res, fmt.Errorf("scan sqlite_master: %w", err)
		}
		if strings.HasPrefix(name, "sqlite_") {
			continue
		}
		if name == "schema_version" {
			continue
		}
		dbTables[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return res, err
	}

	// Unexpected objects are tables present in DB but not represented in migrations.
	for t := range dbTables {
		if _, ok := migrationTables[t]; !ok {
			res.UnexpectedObjects = append(res.UnexpectedObjects, t)
		}
	}

	// Column diffs: compare columns declared in migrations vs those in the DB.
	for t := range migrationTables {
		// only compare when table exists in DB
		if _, ok := dbTables[t]; !ok {
			continue
		}

		pragma := fmt.Sprintf("PRAGMA table_info(%s)", t)
		colRows, err := db.Query(pragma)
		if err != nil {
			// best-effort: skip table on error
			continue
		}
		colsInDB := make(map[string]struct{})
		for colRows.Next() {
			var cid int
			var name, typ string
			var notnull int
			var dflt sql.NullString
			var pk int
			if err := colRows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err == nil {
				colsInDB[name] = struct{}{}
			}
		}
		_ = colRows.Close()

		migCols := migrationCols[t]
		var missing []string
		var extra []string
		for col := range migCols {
			if _, ok := colsInDB[col]; !ok {
				missing = append(missing, col)
			}
		}
		for col := range colsInDB {
			if _, ok := migCols[col]; !ok {
				extra = append(extra, col)
			}
		}
		if len(missing) > 0 || len(extra) > 0 {
			res.ColumnDiffs = append(res.ColumnDiffs, ColumnDiff{Table: t, MissingColumns: missing, ExtraColumns: extra})
			for _, c := range missing {
				def := migrationCols[t][c]
				if strings.TrimSpace(def) != "" {
					res.SuggestedMigrations = append(res.SuggestedMigrations, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", t, def))
				} else {
					res.SuggestedMigrations = append(res.SuggestedMigrations, fmt.Sprintf("-- unknown column type: ALTER TABLE %s ADD COLUMN %s <TYPE>", t, c))
				}
			}
		}
	}

	return res, nil
}

// parseMigrations performs a best-effort parse of migration SQL strings to extract
// table names and column definitions. The parser is intentionally conservative
// (only CREATE TABLE and ALTER TABLE ... ADD COLUMN are inspected) and is used
// solely for advisory/reporting purposes.
func parseMigrations(migs []Migration) (map[string]struct{}, map[string]map[string]string) {
	tables := make(map[string]struct{})
	cols := make(map[string]map[string]string)
	for _, m := range migs {
		s := m.SQL
		lower := strings.ToLower(s)
		idx := 0
		for {
			pos := strings.Index(lower[idx:], "create table")
			if pos == -1 {
				break
			}
			pos += idx
			after := pos + len("create table")
			paren := strings.Index(s[after:], "(")
			if paren == -1 {
				idx = after
				continue
			}
			nameSeg := s[after : after+paren]
			// remove optional IF NOT EXISTS
			nameSeg = strings.Replace(strings.ToLower(nameSeg), "if not exists", "", 1)
			rawNameFields := strings.Fields(strings.TrimSpace(nameSeg))
			if len(rawNameFields) == 0 {
				idx = after + paren
				continue
			}
			// extract raw table name from the original slice to preserve casing
			origNameFields := strings.Fields(strings.TrimSpace(s[after : after+paren]))
			rawName := strings.Trim(origNameFields[len(origNameFields)-1], "`\" ")
			tables[rawName] = struct{}{}

			open := after + paren
			j := open
			depth := 0
			for ; j < len(s); j++ {
				c := s[j]
				if c == '(' {
					depth++
				} else if c == ')' {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			if j >= len(s) {
				idx = open + 1
				continue
			}
			block := s[open+1 : j]
			parts := splitIgnoringParens(block)
			colMap := make(map[string]string)
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				low := strings.ToLower(p)
				if strings.HasPrefix(low, "primary") || strings.HasPrefix(low, "constraint") || strings.HasPrefix(low, "unique") || strings.HasPrefix(low, "foreign") || strings.HasPrefix(low, "check") {
					continue
				}
				fields := strings.Fields(p)
				colName := strings.Trim(fields[0], "`\"")
				colMap[colName] = p
			}
			if len(colMap) > 0 {
				if _, ok := cols[rawName]; !ok {
					cols[rawName] = make(map[string]string)
				}
				for k, v := range colMap {
					cols[rawName][k] = v
				}
			}
			idx = j + 1
		}

		// parse ALTER TABLE ... ADD COLUMN
		idx = 0
		for {
			pos := strings.Index(strings.ToLower(s[idx:]), "alter table")
			if pos == -1 {
				break
			}
			pos += idx
			after := pos + len("alter table")
			addIdx := strings.Index(strings.ToLower(s[after:]), "add column")
			if addIdx == -1 {
				idx = after
				continue
			}
			nameSeg := s[after : after+addIdx]
			nameFields := strings.Fields(nameSeg)
			if len(nameFields) == 0 {
				idx = after + addIdx
				continue
			}
			rawName := strings.Trim(nameFields[len(nameFields)-1], "`\" ")
			colDefStart := after + addIdx + len("add column")
			endIdx := strings.Index(s[colDefStart:], ";")
			var colDef string
			if endIdx == -1 {
				nl := strings.Index(s[colDefStart:], "\n")
				if nl == -1 {
					colDef = s[colDefStart:]
					idx = len(s)
				} else {
					colDef = s[colDefStart : colDefStart+nl]
					idx = colDefStart + nl
				}
			} else {
				colDef = s[colDefStart : colDefStart+endIdx]
				idx = colDefStart + endIdx + 1
			}
			colDef = strings.TrimSpace(colDef)
			if colDef == "" {
				continue
			}
			fields := strings.Fields(colDef)
			colName := strings.Trim(fields[0], "`\"")
			if _, ok := cols[rawName]; !ok {
				cols[rawName] = make(map[string]string)
			}
			cols[rawName][colName] = colDef
		}
	}
	return tables, cols
}

// splitIgnoringParens splits a comma-separated list but ignores commas inside parentheses.
func splitIgnoringParens(s string) []string {
	var parts []string
	var sb strings.Builder
	depth := 0
	for _, r := range s {
		if r == '(' {
			depth++
		}
		if r == ')' {
			if depth > 0 {
				depth--
			}
		}
		if r == ',' && depth == 0 {
			parts = append(parts, sb.String())
			sb.Reset()
			continue
		}
		sb.WriteRune(r)
	}
	if sb.Len() > 0 {
		parts = append(parts, sb.String())
	}
	return parts
}
