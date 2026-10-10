package config

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type Username string
type Password string
type DatabaseName string
type SchemaName string
type Permission string
type ConfigPath string
type HostName string
type Port uint16
type SSLMode string

const (
	Usage       Permission = "USAGE"
	Create      Permission = "CREATE"
	All         Permission = "ALL"
	DefaultPath ConfigPath = "/etc/mediamoth/postgres.yaml"
)

type Configuration struct {
	PostgresConnection Connection `mapstructure:"PostgresConnection" yaml:"PostgresConnection"`
	PostgresDatabases  []Database `mapstructure:"PostgresDatabases" yaml:"PostgresDatabases"`
	PostgresUsers      []User     `mapstructure:"PostgresUsers" yaml:"PostgresUsers"`
}

type Connection struct {
	Host     HostName     `mapstructure:"Host" yaml:"Host"`
	Port     Port         `mapstructure:"Port" yaml:"Port"`
	Username Username     `mapstructure:"Username" yaml:"Username"`
	Password Password     `mapstructure:"Password" yaml:"Password"`
	Database DatabaseName `mapstructure:"Database" yaml:"Database"`
	SSLMode  SSLMode      `mapstructure:"SSLMode" yaml:"SSLMode"`
}

type Database struct {
	Name            DatabaseName `mapstructure:"Name" yaml:"Name"`
	PostgresSchemas []Schema     `mapstructure:"PostgresSchemas" yaml:"PostgresSchemas"`
}

type Schema struct {
	Name  SchemaName `mapstructure:"Name" yaml:"Name"`
	Grant []Grant    `mapstructure:"Grant" yaml:"Grant"`
}

type Grant struct {
	Username Username     `mapstructure:"Username" yaml:"Username"`
	Perms    []Permission `mapstructure:"Perms" yaml:"Perms"`
}

type User struct {
	Username Username `mapstructure:"Username" yaml:"Username"`
	Password Password `mapstructure:"Password" yaml:"Password"`
}

func (c Configuration) Validate() error {
	conn := c.PostgresConnection
	if strings.TrimSpace(string(conn.Host)) == "" || strings.ContainsRune(string(conn.Host), 0) {
		return fmt.Errorf("invalid connection host")
	}
	if conn.Port == 0 {
		return fmt.Errorf("invalid connection port")
	}
	if err := validName(string(conn.Username)); err != nil {
		return fmt.Errorf("connection username: %w", err)
	}
	if err := validName(string(conn.Database)); err != nil {
		return fmt.Errorf("connection database: %w", err)
	}
	if strings.ContainsRune(string(conn.Password), 0) {
		return fmt.Errorf("invalid connection password")
	}
	switch conn.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return fmt.Errorf("invalid connection SSLMode")
	}
	users := make(map[Username]bool)
	for i, u := range c.PostgresUsers {
		if err := validName(string(u.Username)); err != nil {
			return fmt.Errorf("PostgresUsers[%d] username: %w", i, err)
		}
		if users[u.Username] {
			return fmt.Errorf("duplicate username %q", u.Username)
		}
		users[u.Username] = true
		if u.Password == "" || strings.ContainsRune(string(u.Password), 0) {
			return fmt.Errorf("PostgresUsers[%d]: invalid password", i)
		}
	}
	databases := make(map[DatabaseName]bool)
	for i, d := range c.PostgresDatabases {
		if err := validName(string(d.Name)); err != nil {
			return fmt.Errorf("PostgresDatabases[%d]: %w", i, err)
		}
		if databases[d.Name] {
			return fmt.Errorf("duplicate database name %q", d.Name)
		}
		databases[d.Name] = true
		schemas := make(map[SchemaName]bool)
		for j, s := range d.PostgresSchemas {
			if err := validName(string(s.Name)); err != nil {
				return fmt.Errorf("PostgresDatabases[%d].PostgresSchemas[%d]: %w", i, j, err)
			}
			if schemas[s.Name] {
				return fmt.Errorf("duplicate schema name %q in %q", s.Name, d.Name)
			}
			schemas[s.Name] = true
			for k, g := range s.Grant {
				if !users[g.Username] {
					return fmt.Errorf("grant username %q is not declared in PostgresUsers", g.Username)
				}
				if len(g.Perms) == 0 {
					return fmt.Errorf("schema %q grant %d: permissions required", s.Name, k)
				}
				for _, p := range g.Perms {
					switch p {
					case Usage, Create, All:
					default:
						return fmt.Errorf("schema %q: invalid schema permission %q", s.Name, p)
					}
				}
			}
		}
	}
	return nil
}

func validName(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > 63 || strings.ContainsRune(name, 0) || !utf8.ValidString(name) {
		return fmt.Errorf("invalid name: must be nonempty UTF-8, at most 63 bytes, without NUL")
	}
	return nil
}
