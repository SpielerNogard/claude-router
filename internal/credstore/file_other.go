//go:build !darwin

package credstore

func source(configDir string) ([]byte, func([]byte) error, error) { return fileSource(configDir) }
