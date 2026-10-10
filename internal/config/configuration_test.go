package config

import (
	"strings"
	"testing"
)

func validConfig() Configuration {
	return Configuration{
		PostgresConnection: Connection{Host: "/var/run/postgresql", Port: 5432, Username: "postgres", Database: "postgres", SSLMode: "disable"},
		PostgresUsers:      []User{{Username: "app", Password: "secret"}},
		PostgresDatabases:  []Database{{Name: "media", PostgresSchemas: []Schema{{Name: "app", Grant: []Grant{{Username: "app", Perms: []Permission{Usage, Create}}}}}}},
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Configuration)
		want   string
	}{
		{"valid", func(c *Configuration) {}, ""},
		{"unknown user", func(c *Configuration) { c.PostgresDatabases[0].PostgresSchemas[0].Grant[0].Username = "missing" }, "not declared"},
		{"table privilege", func(c *Configuration) {
			c.PostgresDatabases[0].PostgresSchemas[0].Grant[0].Perms = []Permission{"SELECT"}
		}, "permission"},
		{"empty permissions", func(c *Configuration) { c.PostgresDatabases[0].PostgresSchemas[0].Grant[0].Perms = nil }, "permission"},
		{"duplicate user", func(c *Configuration) { c.PostgresUsers = append(c.PostgresUsers, c.PostgresUsers[0]) }, "duplicate"},
		{"duplicate database", func(c *Configuration) { c.PostgresDatabases = append(c.PostgresDatabases, c.PostgresDatabases[0]) }, "duplicate"},
		{"duplicate schema", func(c *Configuration) {
			d := &c.PostgresDatabases[0]
			d.PostgresSchemas = append(d.PostgresSchemas, d.PostgresSchemas[0])
		}, "duplicate"},
		{"empty password", func(c *Configuration) { c.PostgresUsers[0].Password = "" }, "password"},
		{"nul password", func(c *Configuration) { c.PostgresUsers[0].Password = "secret\x00value" }, "password"},
		{"empty name", func(c *Configuration) { c.PostgresDatabases[0].Name = " " }, "name"},
		{"nul name", func(c *Configuration) { c.PostgresDatabases[0].Name = "bad\x00name" }, "name"},
		{"long name", func(c *Configuration) { c.PostgresDatabases[0].Name = DatabaseName(strings.Repeat("é", 32)) }, "name"},
		{"invalid utf8", func(c *Configuration) { c.PostgresUsers[0].Username = Username(string([]byte{255})) }, "name"},
		{"zero port", func(c *Configuration) { c.PostgresConnection.Port = 0 }, "port"},
		{"bad ssl", func(c *Configuration) { c.PostgresConnection.SSLMode = "bad" }, "SSLMode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.change(&c)
			err := c.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v; want %q", err, tt.want)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("password leaked")
			}
		})
	}
}
