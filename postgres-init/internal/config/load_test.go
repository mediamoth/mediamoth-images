package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = `PostgresUsers:
  - Username: app
    Password: yaml-secret
PostgresDatabases:
  - Name: media
    PostgresSchemas:
      - Name: app
        Grant:
          - Username: app
            Perms: [USAGE, CREATE]
`

func writeConfig(t *testing.T, data string) ConfigPath {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return ConfigPath(p)
}

func TestLoadIndexedOverrides(t *testing.T) {
	t.Setenv("POSTGRESUSERS__0__PASSWORD", "env-secret")
	t.Setenv("POSTGRESDATABASES__0__NAME", "override")
	t.Setenv("POSTGRESDATABASES__0__POSTGRESSCHEMAS__0__GRANT__0__PERMS__1", "ALL")
	t.Setenv("POSTGRESCONNECTION__PORT", "5433")
	c, err := Load(writeConfig(t, fixture))
	if err != nil {
		t.Fatal(err)
	}
	if c.PostgresUsers[0].Password != "env-secret" || c.PostgresDatabases[0].Name != "override" || c.PostgresDatabases[0].PostgresSchemas[0].Grant[0].Perms[1] != All || c.PostgresConnection.Port != 5433 {
		t.Fatal("indexed overrides not applied")
	}
	if c.PostgresConnection.Host != "/var/run/postgresql" {
		t.Fatal("missing connection default")
	}
}

func TestLoadRejectsInvalidInput(t *testing.T) {
	for _, tt := range []struct{ name, data, env, value string }{
		{"unknown yaml", fixture + "Misspelled: true\n", "", ""},
		{"malformed yaml", "PostgresUsers: [", "", ""},
		{"bad index", fixture, "POSTGRESUSERS__9__PASSWORD", "new"},
		{"unknown field", fixture, "POSTGRESUSERS__0__PASWORD", "new"},
		{"negative index", fixture, "POSTGRESUSERS__-1__PASSWORD", "new"},
		{"bad port", fixture, "POSTGRESCONNECTION__PORT", "-1"},
		{"overflow port", fixture, "POSTGRESCONNECTION__PORT", "65536"},
		{"empty override", fixture, "POSTGRESUSERS__0__PASSWORD", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv(tt.env, tt.value)
			}
			_, err := Load(writeConfig(t, tt.data))
			if err == nil {
				t.Fatal("expected failure")
			}
			if strings.Contains(err.Error(), "yaml-secret") {
				t.Fatal("password leaked")
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(ConfigPath(filepath.Join(t.TempDir(), "missing.yaml"))); err == nil {
		t.Fatal("expected failure")
	}
}

func TestLoadRejectsInvalidYAMLPort(t *testing.T) {
	for _, port := range []string{"65537", "5432.5", "-1"} {
		t.Run(port, func(t *testing.T) {
			_, err := Load(writeConfig(t, fixture+"PostgresConnection:\n  Port: "+port+"\n"))
			if err == nil {
				t.Fatalf("invalid YAML port %s accepted", port)
			}
		})
	}
}

func TestLoadMalformedYAMLDoesNotExposePassword(t *testing.T) {
	secret := "dummy-password-must-not-appear"
	_, err := Load(writeConfig(t, "PostgresUsers:\n  - Username: app\n    Password: !!int "+secret+"\n"))
	if err == nil {
		t.Fatal("expected parser failure")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("parser error exposed password")
	}
}
