// Package pgx5 provides additive PostgreSQL resource initialization.
package pgx5

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/mediamoth/mediamoth-images/postgres-init/internal/config"
)

func Init(ctx context.Context, cfg config.Configuration) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	connCfg, err := connectionConfig(cfg.PostgresConnection)
	if err != nil {
		return err
	}
	conn, err := pgx.ConnectConfig(ctx, connCfg)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer conn.Close(context.Background())
	for _, user := range cfg.PostgresUsers {
		var exists bool
		if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", user.Username.String()).Scan(&exists); err != nil {
			return fmt.Errorf("check user %q: %w", user.Username, err)
		}
		if exists {
			continue
		}
		verifier, err := passwordVerifier(user.Password)
		if err != nil {
			return fmt.Errorf("prepare password for user %q: %w", user.Username, err)
		}
		// SCRAM verifiers contain only an ASCII prefix, integers, and base64.
		if _, err := conn.Exec(ctx, "CREATE USER "+identifier(user.Username.String())+" WITH PASSWORD '"+string(verifier)+"'"); err != nil {
			return fmt.Errorf("create user %q: %w", user.Username, err)
		}
	}
	for _, database := range cfg.PostgresDatabases {
		var exists bool
		if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", database.Name.String()).Scan(&exists); err != nil {
			return fmt.Errorf("check database %q: %w", database.Name, err)
		}
		if !exists {
			if _, err := conn.Exec(ctx, "CREATE DATABASE "+identifier(database.Name.String())); err != nil {
				return fmt.Errorf("create database %q: %w", database.Name, err)
			}
		}
		for _, grant := range database.Grant {
			statement := "GRANT " + grantPermissions(grant.Perms) + " ON DATABASE " + identifier(database.Name.String()) + " TO " + identifier(grant.Username.String())
			if _, err := conn.Exec(ctx, statement); err != nil {
				return fmt.Errorf("grant database %q to %q: %w", database.Name, grant.Username, err)
			}
		}
		dbCfg := connCfg.Copy()
		dbCfg.Database = database.Name.String()
		if err := initSchemas(ctx, dbCfg, database); err != nil {
			return err
		}
	}
	return nil
}

func initSchemas(ctx context.Context, connCfg *pgx.ConnConfig, database config.Database) error {
	if len(database.PostgresSchemas) == 0 {
		return nil
	}
	conn, err := pgx.ConnectConfig(ctx, connCfg)
	if err != nil {
		return fmt.Errorf("connect to database %q: %w", database.Name, err)
	}
	defer conn.Close(context.Background())
	for _, schema := range database.PostgresSchemas {
		if _, err := conn.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+identifier(schema.Name.String())); err != nil {
			return fmt.Errorf("create schema %q in %q: %w", schema.Name, database.Name, err)
		}
		for _, grant := range schema.Grant {
			statement := "GRANT " + grantPermissions(grant.Perms) + " ON SCHEMA " + identifier(schema.Name.String()) + " TO " + identifier(grant.Username.String())
			if _, err := conn.Exec(ctx, statement); err != nil {
				return fmt.Errorf("grant schema %q in %q to %q: %w", schema.Name, database.Name, grant.Username, err)
			}
		}
	}
	return nil
}

func grantPermissions(permissions []config.Permission) string {
	values := make([]string, len(permissions))
	for i, permission := range permissions {
		// PostgreSQL requires ALL to stand alone, even with redundant privileges.
		if permission == config.All {
			return string(config.All)
		}
		values[i] = string(permission)
	}
	return strings.Join(values, ", ")
}

func identifier(value string) string { return pgx.Identifier{value}.Sanitize() }

func connectionConfig(c config.Connection) (*pgx.ConnConfig, error) {
	uri := url.URL{Scheme: "postgres", User: url.UserPassword(c.Username.String(), string(c.Password)), Path: "/" + c.Database.String()}
	query := url.Values{"host": {string(c.Host)}, "port": {strconv.Itoa(int(c.Port))}, "sslmode": {string(c.SSLMode)}, "connect_timeout": {"10"}}
	uri.RawQuery = query.Encode()
	cfg, err := pgx.ParseConfig(uri.String())
	if err != nil {
		return nil, fmt.Errorf("invalid PostgreSQL connection configuration")
	}
	return cfg, nil
}
