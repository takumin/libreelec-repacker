package libreelec_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/takumin/libreelec-repacker/internal/diskimage"
	"github.com/takumin/libreelec-repacker/internal/diskimage/diskimagetest"
	"github.com/takumin/libreelec-repacker/internal/libreelec"
)

const mib = 1 << 20

// system is a minimal stand-in for the LibreELEC SYSTEM file: only the
// SquashFS magic is checked.
var system = append([]byte("hsqs"), make([]byte, 60)...)

// libreelecLayout mimics the layout of an official image: a FAT boot
// filesystem holding SYSTEM followed by a STORAGE partition.
func libreelecLayout(table diskimage.TableType, bootFS diskimage.FilesystemType, files map[string][]byte) diskimagetest.Layout {
	bootSize := int64(16 * mib)
	if bootFS == diskimage.FilesystemFAT32 {
		bootSize = 40 * mib
	}
	return diskimagetest.Layout{
		Table: table,
		Partitions: []diskimagetest.Partition{
			{Size: bootSize, Filesystem: bootFS, Files: files},
			{Size: 2 * mib},
		},
	}
}

func partitions(l diskimagetest.Layout) []diskimage.Partition {
	parts := make([]diskimage.Partition, len(l.Partitions))
	for i, p := range l.Partitions {
		fsType := p.Filesystem
		if fsType == "" {
			fsType = diskimage.FilesystemUnknown
		}
		parts[i] = diskimage.Partition{Index: i + 1, Start: l.Start(i), Size: p.Size, Filesystem: fsType}
	}
	return parts
}

func TestInspect(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{"SYSTEM": system, "KERNEL": []byte("kernel")}
	// The boot filesystem is found by content, not by partition index.
	bootSecond := diskimagetest.Layout{
		Table: diskimage.TableMBR,
		Partitions: []diskimagetest.Partition{
			{Size: 16 * mib, Filesystem: diskimage.FilesystemFAT16, Files: map[string][]byte{"README": []byte("x")}},
			{Size: 16 * mib, Filesystem: diskimage.FilesystemFAT16, Files: files},
		},
	}

	cases := map[string]struct {
		layout diskimagetest.Layout
		gzip   bool
		boot   int
	}{
		"mbr-fat16":   {layout: libreelecLayout(diskimage.TableMBR, diskimage.FilesystemFAT16, files), boot: 1},
		"mbr-fat32":   {layout: libreelecLayout(diskimage.TableMBR, diskimage.FilesystemFAT32, files), boot: 1},
		"gpt-fat16":   {layout: libreelecLayout(diskimage.TableGPT, diskimage.FilesystemFAT16, files), boot: 1},
		"gzip":        {layout: libreelecLayout(diskimage.TableMBR, diskimage.FilesystemFAT16, files), gzip: true, boot: 1},
		"boot-second": {layout: bootSecond, boot: 2},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "disk.img")
			diskimagetest.Write(t, path, tt.layout)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			format := diskimage.FormatRaw
			if tt.gzip {
				raw := path
				path = filepath.Join(dir, "disk.img.gz")
				diskimagetest.WriteGzip(t, path, raw)
				format = diskimage.FormatGzip
			}

			got, err := libreelec.Inspect(context.Background(), path)
			if err != nil {
				t.Fatalf("Inspect() error = %v", err)
			}
			want := &libreelec.Inspection{
				Format: format,
				Size:   info.Size(),
				PartitionTable: &diskimage.PartitionTable{
					Type:       tt.layout.Table,
					Partitions: partitions(tt.layout),
				},
				Boot: &libreelec.Boot{
					PartitionIndex: tt.boot,
					SystemSize:     int64(len(system)),
					SystemSquashFS: true,
				},
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Inspect() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestInspectError(t *testing.T) {
	t.Parallel()

	withSystem := func(content []byte) map[string][]byte {
		return map[string][]byte{"SYSTEM": content}
	}
	fat16 := diskimage.FilesystemFAT16

	cases := map[string]struct {
		layout diskimagetest.Layout
		is     error
		// boot is the expected partial Boot, if any.
		boot *libreelec.Boot
	}{
		"no-partition-table": {
			layout: diskimagetest.Layout{Size: 4 * mib},
			is:     diskimage.ErrNoPartitionTable,
		},
		"no-fat": {
			layout: libreelecLayout(diskimage.TableMBR, "", nil),
			is:     libreelec.ErrBootNotFound,
		},
		"fat-without-system": {
			layout: libreelecLayout(diskimage.TableMBR, fat16, map[string][]byte{"KERNEL": []byte("kernel")}),
			is:     libreelec.ErrBootNotFound,
		},
		"system-invalid-magic": {
			layout: libreelecLayout(diskimage.TableMBR, fat16, withSystem([]byte("not squashfs"))),
			is:     libreelec.ErrInvalidSystem,
			boot:   &libreelec.Boot{PartitionIndex: 1, SystemSize: int64(len("not squashfs"))},
		},
		"system-shorter-than-magic": {
			layout: libreelecLayout(diskimage.TableMBR, fat16, withSystem([]byte("hs"))),
			is:     libreelec.ErrInvalidSystem,
			boot:   &libreelec.Boot{PartitionIndex: 1, SystemSize: 2},
		},
		"multiple-boot": {
			layout: diskimagetest.Layout{
				Table: diskimage.TableMBR,
				Partitions: []diskimagetest.Partition{
					{Size: 16 * mib, Filesystem: fat16, Files: withSystem(system)},
					{Size: 16 * mib, Filesystem: fat16, Files: withSystem(system)},
				},
			},
			is: libreelec.ErrMultipleBoot,
		},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "disk.img")
			diskimagetest.Write(t, path, tt.layout)

			got, err := libreelec.Inspect(context.Background(), path)
			if !errors.Is(err, tt.is) {
				t.Fatalf("Inspect() error = %v, want %v", err, tt.is)
			}
			if got == nil {
				t.Fatal("Inspect() returned no partial inspection")
			}
			if tt.layout.Table != "" && got.PartitionTable == nil {
				t.Error("PartitionTable = nil, want partition table")
			}
			if !reflect.DeepEqual(got.Boot, tt.boot) {
				t.Errorf("Boot = %+v, want %+v", got.Boot, tt.boot)
			}
		})
	}
}

func TestInspectOpenError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	malformed := filepath.Join(dir, "malformed.img.gz")
	if err := os.WriteFile(malformed, []byte{0x1f, 0x8b, 0x08, 0x00, 0xde, 0xad}, 0o600); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		path string
		is   error
	}{
		"not-exist":      {filepath.Join(dir, "missing.img"), fs.ErrNotExist},
		"malformed-gzip": {malformed, io.ErrUnexpectedEOF},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := libreelec.Inspect(context.Background(), tt.path)
			if !errors.Is(err, tt.is) {
				t.Errorf("Inspect() error = %v, want %v", err, tt.is)
			}
			if got != nil {
				t.Errorf("Inspect() = %+v, want nil", got)
			}
		})
	}
}

func TestInspectDoesNotModifyImage(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "disk.img")
	diskimagetest.Write(t, path, libreelecLayout(diskimage.TableMBR, diskimage.FilesystemFAT16, map[string][]byte{"SYSTEM": system}))
	before := digest(t, path)

	if _, err := libreelec.Inspect(context.Background(), path); err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	if after := digest(t, path); after != before {
		t.Errorf("image digest changed: before %x, after %x", before, after)
	}
}

func digest(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- test fixture path
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}
