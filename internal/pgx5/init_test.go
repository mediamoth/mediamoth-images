package pgx5

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mediamoth/mediamoth-images/internal/config"
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
	cfg.PostgresDatabases[0].PostgresSchemas = append(cfg.PostgresDatabases[0].PostgresSchemas, config.Schema{Name: "added", Grant: []config.Grant{{Username: user, Perms: []config.Permission{config.All}}}})
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
