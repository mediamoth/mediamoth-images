package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

func Load(path ConfigPath) (Configuration, error) {
	v := viper.New()
	v.SetConfigFile(string(path))
	v.SetConfigType("yaml")
	v.AllowEmptyEnv(true)
	for key, value := range map[string]any{
		"PostgresConnection.Host": "/var/run/postgresql", "PostgresConnection.Port": 5432,
		"PostgresConnection.Username": "postgres", "PostgresConnection.Password": "",
		"PostgresConnection.Database": "postgres", "PostgresConnection.SSLMode": "disable",
	} {
		v.SetDefault(key, value)
	}
	if err := v.ReadInConfig(); err != nil {
		// YAML parser errors can embed malformed credential values.
		if _, statErr := os.Stat(string(path)); statErr != nil {
			return Configuration{}, fmt.Errorf("read configuration %q: %w", path, statErr)
		}
		return Configuration{}, fmt.Errorf("parse configuration %q: invalid YAML", path)
	}
	var c Configuration
	if err := v.UnmarshalExact(&c, func(dc *mapstructure.DecoderConfig) { dc.WeaklyTypedInput = false }, viper.DecodeHook(portDecodeHook)); err != nil {
		// Decoder errors can contain credential values. Keep them out of output.
		return Configuration{}, fmt.Errorf("decode configuration %q: unknown field or invalid value", path)
	}
	leaves := make(map[string]reflect.Value)
	configLeaves(reflect.ValueOf(&c).Elem(), nil, leaves)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		root, _, nested := strings.Cut(name, "__")
		if !nested || (root != "POSTGRESCONNECTION" && root != "POSTGRESUSERS" && root != "POSTGRESDATABASES") {
			continue
		}
		field, ok := leaves[name]
		if !ok {
			return Configuration{}, fmt.Errorf("unknown configuration override %q", name)
		}
		key := strings.ReplaceAll(name, "__", ".")
		if err := v.BindEnv(key, name); err != nil {
			return Configuration{}, fmt.Errorf("bind configuration override %q", name)
		}
		value := v.GetString(key)
		switch field.Kind() {
		case reflect.String:
			field.SetString(value)
		case reflect.Uint16:
			port, err := strconv.ParseUint(value, 10, 16)
			if err != nil {
				return Configuration{}, fmt.Errorf("invalid connection port override %q", name)
			}
			field.SetUint(port)
		}
	}
	if err := c.Validate(); err != nil {
		return Configuration{}, err
	}
	return c, nil
}

// Viper does not unmarshal indexed slice environment values automatically.
// Bind each existing leaf using its struct tag and zero-based list indices.
func configLeaves(value reflect.Value, path []string, leaves map[string]reflect.Value) {
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			tag := value.Type().Field(i).Tag.Get("mapstructure")
			configLeaves(value.Field(i), appendPath(path, tag), leaves)
		}
	case reflect.Slice:
		for i := 0; i < value.Len(); i++ {
			configLeaves(value.Index(i), appendPath(path, strconv.Itoa(i)), leaves)
		}
	default:
		leaves[strings.ToUpper(strings.Join(path, "__"))] = value
	}
}

func appendPath(path []string, part string) []string {
	return append(append([]string(nil), path...), part)
}

// Check the YAML number before mapstructure can narrow it to uint16.
func portDecodeHook(from, to reflect.Type, data any) (any, error) {
	if to != reflect.TypeFor[Port]() {
		return data, nil
	}
	switch from.Kind() {
	case reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint64:
	default:
		return nil, fmt.Errorf("port must be an integer")
	}
	port, err := strconv.ParseUint(fmt.Sprint(data), 10, 16)
	if err != nil {
		return nil, fmt.Errorf("port must be between 1 and 65535")
	}
	return Port(port), nil
}
