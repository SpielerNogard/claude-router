package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

func Marshal(c *Config) ([]byte, error) {
	// Copy so Marshal doesn't mutate the pointer in-flight requests are using.
	cp := *c
	cp.Server.Listen = cp.Listen
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cp); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Backup first, then write atomically — there is never a half-written config on disk.
func WriteFile(path string, c *Config) error {
	data, err := Marshal(c)
	if err != nil {
		return err
	}
	old, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := writeAtomic(path+".bak", old); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
	case errors.Is(err, fs.ErrNotExist):
		// first version, nothing to back up
	default:
		return fmt.Errorf("read previous config: %w", err)
	}
	return writeAtomic(path, data)
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	// Without a directory sync the entry can be lost on power failure; not possible on Windows, hence best effort.
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// Only the environment variable's name and whether it is set are exposed.
func Redact(c *Config) map[string]any {
	providers := map[string]any{}
	for name, p := range c.Providers {
		providers[name] = map[string]any{
			"format":       p.Format,
			"auth":         p.Auth,
			"base_url":     p.BaseURL,
			"api_key_env":  p.APIKeyEnv,
			"key_set":      p.APIKeyEnv != "" && os.Getenv(p.APIKeyEnv) != "",
			"models":       p.Models,
			"extra_models": p.ExtraModels,
			"include":      p.Include,
			"exclude":      p.Exclude,
			"fallback":     p.Fallback,
			"default":      p.Default,
		}
	}
	return map[string]any{
		"listen":    c.Listen,
		"log":       c.Log,
		"providers": providers,
	}
}
