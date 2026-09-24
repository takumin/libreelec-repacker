package inspect_test

import (
	"bytes"
	"context"
	"encoding/json"
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

// indentJSON re-indents tab-indented JSON with two spaces to match the
// inspect output, so that expectations can follow .editorconfig.
func indentJSON(t *testing.T, s string) string {
	t.Helper()

	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		t.Fatalf("json.Indent: %v", err)
	}
	return buf.String()
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

func TestInspectOutputPretty(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "disk.img")
	diskimagetest.Write(t, path, diskimagetest.Layout{
		Table: diskimage.TableMBR,
		Partitions: []diskimagetest.Partition{
			{Size: 16 * mib, Filesystem: diskimage.FilesystemFAT16, Files: map[string][]byte{"SYSTEM": []byte("hsqs")}},
		},
	})

	want, err := run(path)
	if err != nil {
		t.Fatalf("inspect error = %v", err)
	}
	for _, args := range [][]string{{"--output", "pretty", path}, {"-o", "pretty", path}} {
		got, err := run(args...)
		if err != nil {
			t.Fatalf("inspect %v error = %v", args, err)
		}
		if got != want {
			t.Errorf("inspect %v output mismatch\ngot:\n%s\nwant:\n%s", args, got, want)
		}
	}
}

func TestInspectJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	system := []byte("hsqs0123")
	layout := diskimagetest.Layout{
		Table: diskimage.TableMBR,
		Partitions: []diskimagetest.Partition{
			{Size: 16 * mib, Filesystem: diskimage.FilesystemFAT16, Files: map[string][]byte{"SYSTEM": system}},
			{Size: 2 * mib},
		},
	}
	valid := filepath.Join(dir, "valid.img")
	diskimagetest.Write(t, valid, layout)

	invalidLayout := diskimagetest.Layout{
		Table: diskimage.TableMBR,
		Partitions: []diskimagetest.Partition{
			{Size: 16 * mib, Filesystem: diskimage.FilesystemFAT16, Files: map[string][]byte{"SYSTEM": []byte("junk")}},
		},
	}
	invalid := filepath.Join(dir, "invalid.img")
	diskimagetest.Write(t, invalid, invalidLayout)

	noBootLayout := diskimagetest.Layout{
		Table:      diskimage.TableMBR,
		Partitions: []diskimagetest.Partition{{Size: 2 * mib}},
	}
	noBoot := filepath.Join(dir, "no-boot.img")
	diskimagetest.Write(t, noBoot, noBootLayout)

	cases := map[string]struct {
		path    string
		wantErr bool
		want    string
	}{
		"valid": {
			path: valid,
			want: fmt.Sprintf(`{
	"image": %q,
	"format": "raw",
	"size": %d,
	"partition_table": {
		"type": "mbr",
		"partitions": [
			{
				"index": 1,
				"start": 1048576,
				"size": 16777216,
				"filesystem": "fat16"
			},
			{
				"index": 2,
				"start": 17825792,
				"size": 2097152,
				"filesystem": "unknown"
			}
		]
	},
	"boot": {
		"partition_index": 1,
		"system_size": 8,
		"system_squashfs": true
	}
}
`, valid, layout.Start(2)+mib),
		},
		"invalid-system": {
			path:    invalid,
			wantErr: true,
			want: fmt.Sprintf(`{
	"image": %q,
	"format": "raw",
	"size": %d,
	"partition_table": {
		"type": "mbr",
		"partitions": [
			{
				"index": 1,
				"start": 1048576,
				"size": 16777216,
				"filesystem": "fat16"
			}
		]
	},
	"boot": {
		"partition_index": 1,
		"system_size": 4,
		"system_squashfs": false
	}
}
`, invalid, invalidLayout.Start(1)+mib),
		},
		"boot-not-found": {
			path:    noBoot,
			wantErr: true,
			want: fmt.Sprintf(`{
	"image": %q,
	"format": "raw",
	"size": %d,
	"partition_table": {
		"type": "mbr",
		"partitions": [
			{
				"index": 1,
				"start": 1048576,
				"size": 2097152,
				"filesystem": "unknown"
			}
		]
	},
	"boot": null
}
`, noBoot, noBootLayout.Start(1)+mib),
		},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := run("--output", "json", tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("inspect error = %v, wantErr %v", err, tt.wantErr)
			}
			if !json.Valid([]byte(got)) {
				t.Errorf("output is not valid JSON:\n%s", got)
			}
			if want := indentJSON(t, tt.want); got != want {
				t.Errorf("output mismatch\ngot:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestInspectUnknownOutput(t *testing.T) {
	t.Parallel()

	got, err := run("--output", "yaml", filepath.Join(t.TempDir(), "missing.img"))
	if err == nil {
		t.Fatal("inspect error = nil, want error")
	}
	if want := "unknown output format: yaml"; !strings.Contains(got, want) {
		t.Errorf("output does not contain %q:\n%s", want, got)
	}
	if strings.Contains(got, "Image:") {
		t.Errorf("unexpected report:\n%s", got)
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
