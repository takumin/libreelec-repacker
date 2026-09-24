package command_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takumin/libreelec-repacker/internal/command"
)

func TestRun(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		stdout string
		stderr string
		stdin  string
		args   string
		exit   int
	}{
		"empty":              {"", "", "", "", command.ExitOK},
		"unknown":            {"", "", "", "a unknown", command.ExitNG},
		"log-level-debug":    {"", "", "", "a -l debug", command.ExitOK},
		"log-level-info":     {"", "", "", "a -l info", command.ExitOK},
		"log-level-warn":     {"", "", "", "a -l warn", command.ExitOK},
		"log-level-error":    {"", "", "", "a -l error", command.ExitOK},
		"log-level-unknown":  {"", "", "", "a -l unknown", command.ExitNG},
		"log-format-text":    {"", "", "", "a -f text", command.ExitOK},
		"log-format-json":    {"", "", "", "a -f json", command.ExitOK},
		"log-format-unknown": {"", "", "", "a -f unknown", command.ExitNG},
		"inspect-no-image":   {"", "", "", "a inspect", command.ExitNG},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			stdin := strings.NewReader(tt.stdin)
			args := strings.Split(tt.args, " ")
			exit := command.Main(&stdout, &stderr, stdin, args)

			switch {
			case tt.exit == command.ExitOK && exit == command.ExitNG:
				t.Error("unexpected error:", stdout, stderr)
			case tt.exit == command.ExitNG && exit == command.ExitOK:
				t.Error("unexpected error:", stdout, stderr)
			}
		})
	}
}

// TestLogOutput is not parallel because the log format flag replaces the
// global default logger.
func TestLogOutput(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			missing := filepath.Join(t.TempDir(), "missing.img")
			args := []string{"a", "-f", format, "inspect", "-o", "json", missing}
			if exit := command.Main(&stdout, &stderr, strings.NewReader(""), args); exit != command.ExitNG {
				t.Fatalf("exit = %d, want %d", exit, command.ExitNG)
			}
			if stdout.Len() != 0 {
				t.Errorf("unexpected stdout:\n%s", stdout.String())
			}
			if want := "failed application"; !strings.Contains(stderr.String(), want) {
				t.Errorf("stderr does not contain %q:\n%s", want, stderr.String())
			}
		})
	}
}
