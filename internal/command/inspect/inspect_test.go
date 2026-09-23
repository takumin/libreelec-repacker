package inspect_test

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/takumin/libreelec-repacker/internal/command/inspect"
	"github.com/takumin/libreelec-repacker/internal/diskimage"
	"github.com/takumin/libreelec-repacker/internal/diskimage/diskimagetest"
)

const mib = 1 << 20

func run(args ...string) (string, error) {
	var stdout bytes.Buffer
	app := &cli.Command{
		Name:           "app",
		Commands:       []*cli.Command{inspect.NewCommands(nil)},
		Writer:         &stdout,
		ErrWriter:      &stdout,
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
	}
	err := app.Run(context.Background(), append([]string{"app", "inspect"}, args...))
	return stdout.String(), err
}

func TestInspect(t *testing.T) {
	t.Parallel()

	system := []byte("hsqs0123")
	layout := diskimagetest.Layout{
		Table: diskimage.TableMBR,
		Partitions: []diskimagetest.Partition{
			{Size: 16 * mib, Filesystem: diskimage.FilesystemFAT16, Files: map[string][]byte{"SYSTEM": system}},
			{Size: 2 * mib},
		},
	}
	path := filepath.Join(t.TempDir(), "disk.img")
	diskimagetest.Write(t, path, layout)

	got, err := run(path)
	if err != nil {
		t.Fatalf("inspect error = %v", err)
	}

	want := strings.Join([]string{
		"Image:            " + path,
		"Format:           raw",
		fmt.Sprintf("Size:             %d bytes", layout.Start(2)+mib),
		"Partition table:  mbr",
		"",
		"Partitions:",
		"  INDEX  START     SIZE      FILESYSTEM",
		"  1      1048576   16777216  fat16",
		"  2      17825792  2097152   unknown",
		"",
		"LibreELEC:",
		"  Boot partition:         1",
		"  SYSTEM size:            8 bytes",
		"  SYSTEM SquashFS magic:  valid",
		"",
	}, "\n")
	if got != want {
		t.Errorf("output mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestInspectError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid.img")
	diskimagetest.Write(t, invalid, diskimagetest.Layout{
		Table: diskimage.TableMBR,
		Partitions: []diskimagetest.Partition{
			{Size: 16 * mib, Filesystem: diskimage.FilesystemFAT16, Files: map[string][]byte{"SYSTEM": []byte("junk")}},
		},
	})

	cases := map[string]struct {
		args []string
		// output is a line the partial report must contain; empty means no
		// report.
		output string
	}{
		"no-args":        {args: nil},
		"too-many-args":  {args: []string{invalid, invalid}},
		"not-exist":      {args: []string{filepath.Join(dir, "missing.img")}},
		"invalid-system": {args: []string{invalid}, output: "  SYSTEM SquashFS magic:  invalid\n"},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := run(tt.args...)
			if err == nil {
				t.Fatal("inspect error = nil, want error")
			}
			if tt.output == "" && got != "" {
				t.Errorf("unexpected output:\n%s", got)
			}
			if !strings.Contains(got, tt.output) {
				t.Errorf("output does not contain %q:\n%s", tt.output, got)
			}
		})
	}
}
