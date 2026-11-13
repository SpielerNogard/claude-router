package main

import (
	"bytes"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestHelpListsAllCommands(t *testing.T) {
	out, err := run(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"stop", "check", "--config", "--data", "--debug"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from help:\n%s", want, out)
		}
	}
}

func TestSubcommandHasOwnHelp(t *testing.T) {
	out, err := run(t, "check", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "passthrough") {
		t.Errorf("check --help does not explain the limitation:\n%s", out)
	}
}

func TestUnknownCommandIsRejected(t *testing.T) {
	if _, err := run(t, "doesnotexist"); err == nil {
		t.Fatal("an unknown command must be reported instead of starting the server")
	}
}

func TestFlagsWorkBeforeAndAfterSubcommand(t *testing.T) {
	for _, args := range [][]string{
		{"check", "--config", "/path/x.toml"},
		{"--config", "/path/x.toml", "check"},
	} {
		flagConfig = ""
		// The call fails on the missing file; what matters is that the flag was parsed.
		run(t, args...)
		if flagConfig != "/path/x.toml" {
			t.Errorf("%v: config = %q, want /path/x.toml", args, flagConfig)
		}
	}
}

func TestCheckAcceptsAtMostOneProvider(t *testing.T) {
	if _, err := run(t, "check", "a", "b"); err == nil {
		t.Fatal("two provider names must be rejected")
	}
}
