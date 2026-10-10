package pgx5

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mediamoth/mediamoth-images/postgres-init/internal/config"
)

func TestInitPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set TEST_POSTGRES_DSN to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	user := config.Username(`user"` + suffix)
	database := config.DatabaseName(`db"` + suffix)
	schema := config.SchemaName(`schema"` + suffix)
	password := config.Password(`pass'\word`)
	cfg := config.Configuration{
		PostgresConnection: config.Connection{Host: config.HostName(parsed.Host), Port: config.Port(parsed.Port), Username: config.Username(parsed.User), Password: config.Password(parsed.Password), Database: config.DatabaseName(parsed.Database), SSLMode: "disable"},
		PostgresUsers:      []config.User{{Username: user, Password: password}},
		PostgresDatabases:  []config.Database{{Name: database, PostgresSchemas: []config.Schema{{Name: schema, Grant: []config.Grant{{Username: user, Perms: []config.Permission{config.Usage, config.Create}}}}}}},
	}
	defer func() {
		conn.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{string(database)}.Sanitize()+" WITH (FORCE)")
		conn.Exec(context.Background(), "DROP ROLE IF EXISTS "+pgx.Identifier{string(user)}.Sanitize())
	}()
	if err := Init(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	var savedPassword string
	if err := conn.QueryRow(ctx, "SELECT rolpassword FROM pg_authid WHERE rolname=$1", string(user)).Scan(&savedPassword); err != nil {
		t.Fatal(err)
	}
	cfg.PostgresUsers[0].Password = "changed-password"
	if err := Init(ctx, cfg); err != nil {
		t.Fatalf("repeat init: %v", err)
	}
	var afterPassword string
	if err := conn.QueryRow(ctx, "SELECT rolpassword FROM pg_authid WHERE rolname=$1", string(user)).Scan(&afterPassword); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(savedPassword, "SCRAM-SHA-256$") {
		t.Fatal("password not stored as SCRAM verifier")
	}
	if savedPassword != afterPassword {
		t.Fatal("existing password changed")
	}
	dbCfg := parsed.Copy()
	dbCfg.Database = string(database)
	db, err := pgx.ConnectConfig(ctx, dbCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	for _, perm := range []string{"USAGE", "CREATE"} {
		var granted bool
		if err := db.QueryRow(ctx, "SELECT has_schema_privilege($1,$2,$3)", string(user), string(schema), perm).Scan(&granted); err != nil || !granted {
			t.Fatalf("missing %s: %v", perm, err)
		}
	}
	table := pgx.Identifier{string(schema), "existing_table"}.Sanitize()
	if _, err := db.Exec(ctx, "CREATE TABLE "+table+" (id integer)"); err != nil {
		t.Fatal(err)
	}
	if err := Init(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	var tableAccess bool
	if err := db.QueryRow(ctx, "SELECT has_table_privilege($1,$2,'SELECT')", string(user), table).Scan(&tableAccess); err != nil {
		t.Fatal(err)
	}
	if tableAccess {
		t.Fatal("schema grant also granted table access")
	}
	loginCfg := dbCfg.Copy()
	loginCfg.User = string(user)
	loginCfg.Password = string(password)
	login, err := pgx.ConnectConfig(ctx, loginCfg)
	if err != nil {
		t.Fatalf("quoted password login: %v", err)
	}
	login.Close(ctx)
	cfg.PostgresDatabases[0].PostgresSchemas = append(cfg.PostgresDatabases[0].PostgresSchemas, config.Schema{Name: "added", Grant: []config.Grant{{Username: user, Perms: []config.Permission{config.All, config.Create, config.All}}}})
	if err := Init(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname='added')").Scan(&exists); err != nil || !exists {
		t.Fatalf("new schema missing: %v", err)
	}
	var owner string
	if err := conn.QueryRow(ctx, "SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname=$1", string(database)).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != parsed.User {
		t.Fatalf("ownership changed: %s", owner)
	}
	// Invalid grants must be rejected before even the first user is created.
	invalid := cfg
	invalid.PostgresUsers = []config.User{{Username: config.Username("invalid" + suffix), Password: "secret"}}
	if err := Init(ctx, invalid); err == nil {
		t.Fatal("invalid grants accepted")
	}
	if err := conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)", string(invalid.PostgresUsers[0].Username)).Scan(&exists); err != nil || exists {
		t.Fatalf("invalid config mutated database: %v", err)
	}
	t.Log(fmt.Sprintf("verified repeat init, quoted SQL, password preservation, additive schema, and schema-only grants in %s", database))
}

func TestPasswordVerifierContainsNoPlaintext(t *testing.T) {
	password := config.Password("dummy-sensitive-password")
	first, err := passwordVerifier(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := passwordVerifier(password)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(first), string(password)) {
		t.Fatal("plaintext password included")
	}
	if !strings.HasPrefix(string(first), "SCRAM-SHA-256$4096:") {
		t.Fatal("invalid PostgreSQL verifier")
	}
	if first == second {
		t.Fatal("password salt reused")
	}
}

func TestInitDatabaseGrants(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set TEST_POSTGRES_DSN to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adminCfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgx.ConnectConfig(ctx, adminCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	user := config.Username(`grant-user"` + suffix)
	database := config.DatabaseName(`grant-db"` + suffix)
	defer func() {
		admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+identifier(database.String())+" WITH (FORCE)")
		admin.Exec(context.Background(), "DROP ROLE IF EXISTS "+identifier(user.String()))
	}()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier(database.String())); err != nil {
		t.Fatal(err)
	}
	// Remove defaults so CONNECT and TEMPORARY cannot pass through PUBLIC.
	if _, err := admin.Exec(ctx, "REVOKE ALL ON DATABASE "+identifier(database.String())+" FROM PUBLIC"); err != nil {
		t.Fatal(err)
	}
	data := fmt.Sprintf("PostgresUsers:\n  - Username: %q\n    Password: test-password\nPostgresDatabases:\n  - Name: %q\n", user.String(), database.String())
	load := func(data string) config.Configuration {
		t.Helper()
		path := filepath.Join(t.TempDir(), "postgres.yaml")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(config.ConfigPath(path))
		if err != nil {
			t.Fatal(err)
		}
		cfg.PostgresConnection = config.Connection{Host: config.HostName(adminCfg.Host), Port: config.Port(adminCfg.Port), Username: config.Username(adminCfg.User), Password: config.Password(adminCfg.Password), Database: config.DatabaseName(adminCfg.Database), SSLMode: "disable"}
		return cfg
	}
	withoutGrants := load(data)
	if err := Init(ctx, withoutGrants); err != nil {
		t.Fatal(err)
	}
	assertPrivileges := func(want bool) {
		t.Helper()
		for _, permission := range []string{"CREATE", "CONNECT", "TEMPORARY"} {
			var got bool
			if err := admin.QueryRow(ctx, "SELECT has_database_privilege($1,$2,$3)", user.String(), database.String(), permission).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("database %s: got %v, want %v", permission, got, want)
			}
		}
	}
	assertPrivileges(false)
	withGrants := load(data + fmt.Sprintf("    Grant:\n      - Username: %q\n        Perms: [CREATE, CONNECT, TEMPORARY]\n", user.String()))
	if err := Init(ctx, withGrants); err != nil {
		t.Fatal(err)
	}
	assertPrivileges(true)
	dbCfg := adminCfg.Copy()
	dbCfg.Database = database.String()
	db, err := pgx.ConnectConfig(ctx, dbCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	if _, err := db.Exec(ctx, "CREATE SCHEMA existing_schema"); err != nil {
		t.Fatal(err)
	}
	dbCfg.User, dbCfg.Password = user.String(), "test-password"
	app, err := pgx.ConnectConfig(ctx, dbCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close(context.Background())
	for _, statement := range []string{"CREATE SCHEMA IF NOT EXISTS existing_schema", "CREATE SCHEMA migration_schema", "CREATE TEMP TABLE temporary_table (id integer)"} {
		if _, err := app.Exec(ctx, statement); err != nil {
			t.Fatalf("application statement %q: %v", statement, err)
		}
	}
	// Reapplying configuration must restore a revoked grant on existing resources.
	if _, err := admin.Exec(ctx, "REVOKE ALL ON DATABASE "+identifier(database.String())+" FROM "+identifier(user.String())); err != nil {
		t.Fatal(err)
	}
	if err := Init(ctx, withGrants); err != nil {
		t.Fatal(err)
	}
	assertPrivileges(true)
	if _, err := admin.Exec(ctx, "REVOKE ALL ON DATABASE "+identifier(database.String())+" FROM "+identifier(user.String())); err != nil {
		t.Fatal(err)
	}
	withAll := load(data + fmt.Sprintf("    Grant:\n      - Username: %q\n        Perms: [ALL, CREATE, CONNECT]\n", user.String()))
	if err := Init(ctx, withAll); err != nil {
		t.Fatal(err)
	}
	assertPrivileges(true)
	if err := Init(ctx, withoutGrants); err != nil {
		t.Fatal(err)
	}
	assertPrivileges(true)
	var owner string
	if err := admin.QueryRow(ctx, "SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname=$1", database.String()).Scan(&owner); err != nil || owner != adminCfg.User {
		t.Fatalf("database ownership changed: %q, %v", owner, err)
	}
}
