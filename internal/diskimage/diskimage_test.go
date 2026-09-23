package diskimage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/takumin/libreelec-repacker/internal/diskimage"
	"github.com/takumin/libreelec-repacker/internal/diskimage/diskimagetest"
)

const mib = 1 << 20

// isolateTempDir points the default temporary directory at an empty directory
// and returns it, so that tests can check that nothing is left behind.
func isolateTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	return dir
}

func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		t.Errorf("temporary file left behind: %s", e.Name())
	}
}

func TestOpen(t *testing.T) {
	src := t.TempDir()
	raw := filepath.Join(src, "disk.img")
	diskimagetest.Write(t, raw, diskimagetest.Layout{
		Table:      diskimage.TableMBR,
		Partitions: []diskimagetest.Partition{{Size: mib}},
	})
	rawInfo, err := os.Stat(raw)
	if err != nil {
		t.Fatal(err)
	}
	gz := filepath.Join(src, "disk.img.gz")
	diskimagetest.WriteGzip(t, gz, raw)
	// Detection must rely on the signature, not on the name.
	gzNoExt := filepath.Join(src, "compressed.img")
	diskimagetest.WriteGzip(t, gzNoExt, raw)

	cases := map[string]struct {
		path   string
		format diskimage.Format
	}{
		"raw":              {raw, diskimage.FormatRaw},
		"gzip":             {gz, diskimage.FormatGzip},
		"gzip-without-.gz": {gzNoExt, diskimage.FormatGzip},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			tmp := isolateTempDir(t)

			img, err := diskimage.Open(context.Background(), tt.path)
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			if got := img.Format(); got != tt.format {
				t.Errorf("Format() = %q, want %q", got, tt.format)
			}
			if got := img.Size(); got != rawInfo.Size() {
				t.Errorf("Size() = %d, want %d", got, rawInfo.Size())
			}
			if err := img.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			assertEmptyDir(t, tmp)
		})
	}
}

func TestOpenError(t *testing.T) {
	src := t.TempDir()
	raw := filepath.Join(src, "disk.img")
	diskimagetest.Write(t, raw, diskimagetest.Layout{
		Table:      diskimage.TableMBR,
		Partitions: []diskimagetest.Partition{{Size: mib}},
	})
	valid := filepath.Join(src, "valid.img.gz")
	diskimagetest.WriteGzip(t, valid, raw)
	compressed, err := os.ReadFile(valid) // #nosec G304 -- test fixture path
	if err != nil {
		t.Fatal(err)
	}

	write := func(name string, content []byte) string {
		path := filepath.Join(src, name)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	corrupted := bytes.Clone(compressed)
	corrupted[len(corrupted)-8] ^= 0xff // CRC-32 in the gzip trailer

	cases := map[string]struct {
		path string
		is   error
	}{
		"not-exist":         {filepath.Join(src, "missing.img"), fs.ErrNotExist},
		"empty":             {write("empty.img", nil), nil},
		"gzip-bad-header":   {write("bad-header.img.gz", []byte{0x1f, 0x8b, 0x00, 0x00}), nil},
		"gzip-truncated":    {write("truncated.img.gz", compressed[:len(compressed)/2]), io.ErrUnexpectedEOF},
		"gzip-bad-checksum": {write("bad-checksum.img.gz", corrupted), nil},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			tmp := isolateTempDir(t)

			img, err := diskimage.Open(context.Background(), tt.path)
			if err == nil {
				_ = img.Close()
				t.Fatal("Open() error = nil, want error")
			}
			if tt.is != nil && !errors.Is(err, tt.is) {
				t.Errorf("Open() error = %v, want %v", err, tt.is)
			}
			assertEmptyDir(t, tmp)
		})
	}
}

func TestOpenCancel(t *testing.T) {
	src := t.TempDir()
	raw := filepath.Join(src, "disk.img")
	diskimagetest.Write(t, raw, diskimagetest.Layout{
		Table:      diskimage.TableMBR,
		Partitions: []diskimagetest.Partition{{Size: mib}},
	})
	gz := filepath.Join(src, "disk.img.gz")
	diskimagetest.WriteGzip(t, gz, raw)
	tmp := isolateTempDir(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	img, err := diskimage.Open(ctx, gz)
	if err == nil {
		_ = img.Close()
		t.Fatal("Open() error = nil, want error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Open() error = %v, want %v", err, context.Canceled)
	}
	assertEmptyDir(t, tmp)
}

func TestPartitionTable(t *testing.T) {
	t.Parallel()

	layout := func(table diskimage.TableType) diskimagetest.Layout {
		return diskimagetest.Layout{
			Table: table,
			Partitions: []diskimagetest.Partition{
				{Size: 16 * mib, Filesystem: diskimage.FilesystemFAT16},
				{Size: 2 * mib},
				{Size: 40 * mib, Filesystem: diskimage.FilesystemFAT32},
			},
		}
	}
	want := func(table diskimage.TableType) *diskimage.PartitionTable {
		l := layout(table)
		return &diskimage.PartitionTable{
			Type: table,
			Partitions: []diskimage.Partition{
				{Index: 1, Start: l.Start(0), Size: 16 * mib, Filesystem: diskimage.FilesystemFAT16},
				{Index: 2, Start: l.Start(1), Size: 2 * mib, Filesystem: diskimage.FilesystemUnknown},
				{Index: 3, Start: l.Start(2), Size: 40 * mib, Filesystem: diskimage.FilesystemFAT32},
			},
		}
	}

	for _, table := range []diskimage.TableType{diskimage.TableMBR, diskimage.TableGPT} {
		t.Run(string(table), func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "disk.img")
			diskimagetest.Write(t, path, layout(table))
			img := open(t, path)

			got, err := img.PartitionTable()
			if err != nil {
				t.Fatalf("PartitionTable() error = %v", err)
			}
			if !reflect.DeepEqual(got, want(table)) {
				t.Errorf("PartitionTable() = %+v, want %+v", got, want(table))
			}
		})
	}
}

func TestPartitionTableExt(t *testing.T) {
	t.Parallel()

	l := diskimagetest.Layout{
		Table:      diskimage.TableMBR,
		Partitions: []diskimagetest.Partition{{Size: mib}},
	}
	path := filepath.Join(t.TempDir(), "disk.img")
	diskimagetest.Write(t, path, l)
	patch(t, path, l.Start(0)+1024+0x38, []byte{0x53, 0xef})
	img := open(t, path)

	got, err := img.PartitionTable()
	if err != nil {
		t.Fatalf("PartitionTable() error = %v", err)
	}
	if fsType := got.Partitions[0].Filesystem; fsType != diskimage.FilesystemExt {
		t.Errorf("Filesystem = %q, want %q", fsType, diskimage.FilesystemExt)
	}
}

func TestPartitionTableError(t *testing.T) {
	t.Parallel()

	t.Run("no-partition-table", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "disk.img")
		diskimagetest.Write(t, path, diskimagetest.Layout{Size: 4 * mib})
		img := open(t, path)

		if _, err := img.PartitionTable(); !errors.Is(err, diskimage.ErrNoPartitionTable) {
			t.Errorf("PartitionTable() error = %v, want %v", err, diskimage.ErrNoPartitionTable)
		}
	})

	t.Run("partition-beyond-end", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "disk.img")
		diskimagetest.Write(t, path, diskimagetest.Layout{
			Table:      diskimage.TableMBR,
			Partitions: []diskimagetest.Partition{{Size: 4 * mib}},
		})
		if err := os.Truncate(path, 2*mib); err != nil {
			t.Fatal(err)
		}
		img := open(t, path)

		if _, err := img.PartitionTable(); err == nil {
			t.Error("PartitionTable() error = nil, want error")
		}
	})
}

func TestFAT(t *testing.T) {
	t.Parallel()

	content := []byte("hello")
	path := filepath.Join(t.TempDir(), "disk.img")
	diskimagetest.Write(t, path, diskimagetest.Layout{
		Table: diskimage.TableMBR,
		Partitions: []diskimagetest.Partition{
			{Size: 2 * mib},
			{Size: 16 * mib, Filesystem: diskimage.FilesystemFAT16, Files: map[string][]byte{"HELLO": content}},
		},
	})
	img := open(t, path)
	table, err := img.PartitionTable()
	if err != nil {
		t.Fatalf("PartitionTable() error = %v", err)
	}

	if _, err := img.OpenFAT(table.Partitions[0]); !errors.Is(err, diskimage.ErrNotFAT) {
		t.Errorf("OpenFAT(unformatted) error = %v, want %v", err, diskimage.ErrNotFAT)
	}

	fat, err := img.OpenFAT(table.Partitions[1])
	if err != nil {
		t.Fatalf("OpenFAT() error = %v", err)
	}

	if _, err := fat.Open("MISSING"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open(MISSING) error = %v, want %v", err, fs.ErrNotExist)
	}

	// FAT names are case-insensitive.
	f, err := fat.Open("hello")
	if err != nil {
		t.Fatalf("Open(hello) error = %v", err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if f.Size() != int64(len(content)) {
		t.Errorf("Size() = %d, want %d", f.Size(), len(content))
	}
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func open(t *testing.T, path string) *diskimage.Image {
	t.Helper()
	img, err := diskimage.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := img.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return img
}

func patch(t *testing.T, path string, off int64, b []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY, 0) // #nosec G304 -- test fixture path
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(b, off); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
