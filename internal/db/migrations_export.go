package db

// GetMigrations returns a copy of the migration definitions for read-only inspection.
func GetMigrations() []Migration {
	out := make([]Migration, len(migrations))
	copy(out, migrations)
	return out
}
