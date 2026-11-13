//go:build darwin

package credstore

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// On macOS the credential lives in the keychain; the file does not exist there.
func source(configDir string) ([]byte, func([]byte) error, error) {
	svc := ServiceName(configDir)
	out, err := exec.Command("security", "find-generic-password", "-s", svc, "-w").Output()
	if err == nil {
		return []byte(strings.TrimSpace(string(out))), func(b []byte) error { return writeKeychain(svc, b) }, nil
	}
	// A second Claude Code install may also store its login as a file.
	if raw, save, ferr := fileSource(configDir); ferr == nil {
		return raw, save, nil
	}
	return nil, nil, fmt.Errorf("keychain entry %q not readable: %w", svc, err)
}

var acctAttr = regexp.MustCompile(`"acct"<blob>="([^"]*)"`)

// Hex via stdin instead of -w as an argument: otherwise the secret would briefly show up in ps.
func writeKeychain(svc string, b []byte) error {
	attrs, err := exec.Command("security", "find-generic-password", "-s", svc).Output()
	if err != nil {
		return fmt.Errorf("keychain entry %q: %w", svc, err)
	}
	m := acctAttr.FindSubmatch(attrs)
	if m == nil {
		return fmt.Errorf("keychain entry %q: no account found", svc)
	}
	cmd := exec.Command("security", "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -a %q -s %q -X %s\n",
		m[1], svc, hex.EncodeToString(b)))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("security: %v: %s", err, out)
	}
	// security -i does not reliably report a failed command in its exit code.
	back, err := exec.Command("security", "find-generic-password", "-s", svc, "-w").Output()
	if err != nil || !bytes.Equal(bytes.TrimSpace(back), b) {
		return fmt.Errorf("keychain entry %q: read-back value differs", svc)
	}
	return nil
}
