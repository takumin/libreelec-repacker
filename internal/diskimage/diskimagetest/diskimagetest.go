// Package diskimagetest builds small disk images for tests.
package diskimagetest

import (
	"compress/gzip"
	"os"
	"testing"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/partition"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/diskfs/go-diskfs/partition/mbr"

	"github.com/takumin/libreelec-repacker/internal/diskimage"
)

const (
	sectorSize = 512
	// alignment is the start of the first partition and the space left at
	// the end of the image for the backup GPT.
	alignment = 1 << 20
)

// Layout describes a disk image to build.
type Layout struct {
	// Table is the partition table type. An empty Table creates an image
	// without a partition table, filled with zeros.
	Table diskimage.TableType
	// Partitions are placed back to back after a 1 MiB gap.
	Partitions []Partition
	// Size is used only when Table is empty.
	Size int64
}

// Partition describes a partition of a Layout.
type Partition struct {
	// Size in bytes; must be a multiple of 512.
	Size int64
	// Filesystem is the FAT variant to create. Any other value leaves the
	// partition filled with zeros.
	Filesystem diskimage.FilesystemType
	// Files are written to the root directory of the FAT filesystem.
	Files map[string][]byte
}

// Start returns the byte offset at which partition i of l is placed.
func (l Layout) Start(i int) int64 {
	start := int64(alignment)
	for _, p := range l.Partitions[:i] {
		start += p.Size
	}
	return start
}

// Write builds the image described by l at path.
func Write(t testing.TB, path string, l Layout) {
	t.Helper()

	if l.Table == "" {
		if err := os.WriteFile(path, make([]byte, l.Size), 0o600); err != nil {
			t.Fatalf("write image: %v", err)
		}
		return
	}

	size := l.Start(len(l.Partitions)) + alignment
	d, err := diskfs.Create(path, size, diskfs.SectorSize512)
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	defer func() {
		if err := d.Close(); err != nil {
			t.Fatalf("close image: %v", err)
		}
	}()

	if err := d.Partition(table(l)); err != nil {
		t.Fatalf("write partition table: %v", err)
	}

	for i, p := range l.Partitions {
		var fsType filesystem.Type
		switch p.Filesystem {
		case diskimage.FilesystemFAT12:
			fsType = filesystem.TypeFat12
		case diskimage.FilesystemFAT16:
			fsType = filesystem.TypeFat16
		case diskimage.FilesystemFAT32:
			fsType = filesystem.TypeFat32
		default:
			continue
		}
		fs, err := d.CreateFilesystem(disk.FilesystemSpec{Partition: i + 1, FSType: fsType, VolumeLabel: "TEST"})
		if err != nil {
			t.Fatalf("create %s on partition %d: %v", p.Filesystem, i+1, err)
		}
		for name, content := range p.Files {
			f, err := fs.OpenFile("/"+name, os.O_CREATE|os.O_RDWR)
			if err != nil {
				t.Fatalf("create %s: %v", name, err)
			}
			if _, err := f.Write(content); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
			if err := f.Close(); err != nil {
				t.Fatalf("close %s: %v", name, err)
			}
		}
	}
}

// table converts the layout into a partition table. Test layouts are far
// below the limits of the sector fields.
//
// #nosec G115
func table(l Layout) partition.Table {
	if l.Table == diskimage.TableGPT {
		t := &gpt.Table{LogicalSectorSize: sectorSize, PhysicalSectorSize: sectorSize, ProtectiveMBR: true}
		for i, p := range l.Partitions {
			start := uint64(l.Start(i) / sectorSize)
			t.Partitions = append(t.Partitions, &gpt.Partition{
				Index: i + 1,
				Start: start,
				End:   start + uint64(p.Size/sectorSize) - 1,
				Type:  gpt.MicrosoftBasicData,
			})
		}
		return t
	}

	t := &mbr.Table{LogicalSectorSize: sectorSize, PhysicalSectorSize: sectorSize}
	for i, p := range l.Partitions {
		t.Partitions = append(t.Partitions, &mbr.Partition{
			Index: i + 1,
			Type:  mbr.Fat32LBA,
			Start: uint32(l.Start(i) / sectorSize),
			Size:  uint32(p.Size / sectorSize),
		})
	}
	return t
}

// WriteGzip writes the gzip-compressed content of the file src to dst.
func WriteGzip(t testing.TB, dst, src string) {
	t.Helper()

	raw, err := os.ReadFile(src) // #nosec G304 -- test fixture path
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	f, err := os.Create(dst) // #nosec G304 -- test fixture path
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	zw := gzip.NewWriter(f)
	if _, err := zw.Write(raw); err != nil {
		t.Fatalf("compress %s: %v", src, err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("compress %s: %v", src, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", dst, err)
	}
}
