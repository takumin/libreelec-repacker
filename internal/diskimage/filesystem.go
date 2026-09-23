package diskimage

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/fat12"
	"github.com/diskfs/go-diskfs/filesystem/fat16"
	"github.com/diskfs/go-diskfs/filesystem/fat32"
)

// ErrNotFAT is returned when a FAT filesystem is requested from a partition
// that does not contain one.
var ErrNotFAT = errors.New("not a FAT filesystem")

// FilesystemType identifies a filesystem by its on-disk signature.
type FilesystemType string

const (
	// FilesystemUnknown means that no supported signature was recognized.
	FilesystemUnknown FilesystemType = "unknown"
	// FilesystemFAT12 is a FAT12 filesystem.
	FilesystemFAT12 FilesystemType = "fat12"
	// FilesystemFAT16 is a FAT16 filesystem.
	FilesystemFAT16 FilesystemType = "fat16"
	// FilesystemFAT32 is a FAT32 filesystem.
	FilesystemFAT32 FilesystemType = "fat32"
	// FilesystemExt is an ext2, ext3 or ext4 filesystem. The variants share
	// a superblock signature and are not told apart.
	FilesystemExt FilesystemType = "ext2/3/4"
)

const (
	// Cluster count limits that define the FAT variant (Microsoft FAT
	// specification).
	fat12MaxClusters = 4085
	fat16MaxClusters = 65525

	// Location of the ext2/3/4 superblock magic.
	extMagicOffset = 1024 + 0x38
	extMagic       = 0xEF53

	// probeSize covers the FAT boot sector and the ext superblock magic.
	probeSize = 2048
)

// detectFilesystem identifies the filesystem stored at [start, start+size)
// from its signature, without parsing the filesystem itself.
func (img *Image) detectFilesystem(start, size int64) (FilesystemType, error) {
	buf := make([]byte, min(size, probeSize))
	if _, err := img.disk.Backend.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read filesystem signature: %w", err)
	}
	if t := fatType(buf, size); t != FilesystemUnknown {
		return t, nil
	}
	if len(buf) >= extMagicOffset+2 && binary.LittleEndian.Uint16(buf[extMagicOffset:]) == extMagic {
		return FilesystemExt, nil
	}
	return FilesystemUnknown, nil
}

// fatType validates the BIOS parameter block in boot sector b of a volume of
// size bytes and derives the FAT variant.
//
// The checks mirror what the Linux FAT driver requires before mounting. They
// also keep values that would make the FAT readers misbehave, such as a zero
// cluster size or a FAT larger than the volume, away from them.
func fatType(b []byte, size int64) FilesystemType {
	if len(b) < 512 {
		return FilesystemUnknown
	}
	bytesPerSector := int64(binary.LittleEndian.Uint16(b[0x0B:]))
	sectorsPerCluster := int64(b[0x0D])
	reservedSectors := int64(binary.LittleEndian.Uint16(b[0x0E:]))
	fatCount := int64(b[0x10])
	rootEntries := int64(binary.LittleEndian.Uint16(b[0x11:]))
	totalSectors := int64(binary.LittleEndian.Uint16(b[0x13:]))
	media := b[0x15]
	sectorsPerFAT := int64(binary.LittleEndian.Uint16(b[0x16:]))
	if totalSectors == 0 {
		totalSectors = int64(binary.LittleEndian.Uint32(b[0x20:]))
	}
	fat32Layout := sectorsPerFAT == 0
	if fat32Layout {
		sectorsPerFAT = int64(binary.LittleEndian.Uint32(b[0x24:]))
	}

	switch {
	case bytesPerSector < 512 || bytesPerSector > 4096 || !powerOfTwo(bytesPerSector),
		sectorsPerCluster == 0 || !powerOfTwo(sectorsPerCluster),
		reservedSectors == 0,
		fatCount == 0,
		media != 0xF0 && media < 0xF8,
		sectorsPerFAT == 0,
		totalSectors == 0 || totalSectors > size/bytesPerSector,
		fat32Layout && rootEntries != 0:
		return FilesystemUnknown
	}

	rootDirSectors := (rootEntries*32 + bytesPerSector - 1) / bytesPerSector
	metaSectors := reservedSectors + fatCount*sectorsPerFAT + rootDirSectors
	if metaSectors >= totalSectors {
		return FilesystemUnknown
	}
	clusters := (totalSectors - metaSectors) / sectorsPerCluster

	switch {
	case fat32Layout:
		return FilesystemFAT32
	case clusters < fat12MaxClusters:
		return FilesystemFAT12
	case clusters < fat16MaxClusters:
		return FilesystemFAT16
	default:
		return FilesystemUnknown
	}
}

func powerOfTwo(n int64) bool {
	return n > 0 && n&(n-1) == 0
}

// FAT is a read-only view of a FAT filesystem inside a disk image.
type FAT struct {
	fs filesystem.FileSystem
}

// OpenFAT opens the FAT filesystem stored in partition p of the image.
//
// It returns an error wrapping ErrNotFAT when p.Filesystem is not a FAT
// variant.
func (img *Image) OpenFAT(p Partition) (*FAT, error) {
	switch p.Filesystem {
	case FilesystemFAT12, FilesystemFAT16, FilesystemFAT32:
	default:
		return nil, fmt.Errorf("partition %d: %w", p.Index, ErrNotFAT)
	}

	// The FAT readers locate the root directory and the data region
	// assuming exactly two FAT copies, so reject anything else instead of
	// misreading the filesystem.
	fatCount := make([]byte, 1)
	if _, err := img.disk.Backend.ReadAt(fatCount, p.Start+0x10); err != nil {
		return nil, fmt.Errorf("partition %d: read FAT count: %w", p.Index, err)
	}
	if fatCount[0] != 2 {
		return nil, fmt.Errorf("partition %d: unsupported FAT count %d", p.Index, fatCount[0])
	}

	var (
		fsys filesystem.FileSystem
		err  error
	)
	backend, blocksize := img.disk.Backend, img.disk.LogicalBlocksize
	switch p.Filesystem {
	case FilesystemFAT12:
		fsys, err = fat12.Read(backend, p.Size, p.Start, blocksize)
	case FilesystemFAT16:
		fsys, err = fat16.Read(backend, p.Size, p.Start, blocksize)
	default:
		fsys, err = fat32.Read(backend, p.Size, p.Start, blocksize)
	}
	if err != nil {
		return nil, fmt.Errorf("partition %d: read %s filesystem: %w", p.Index, p.Filesystem, err)
	}
	return &FAT{fs: fsys}, nil
}

// Open opens the regular file name in the root directory of the filesystem.
//
// Names are matched case-insensitively, as FAT does. It returns an error
// wrapping fs.ErrNotExist when there is no such file.
func (f *FAT) Open(name string) (*File, error) {
	entries, err := f.fs.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("read root directory: %w", err)
	}
	for _, e := range entries {
		if !strings.EqualFold(e.Name(), name) {
			continue
		}
		if e.IsDir() {
			return nil, &fs.PathError{Op: "open", Path: name, Err: errors.New("is a directory")}
		}
		info, err := e.Info()
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", name, err)
		}
		r, err := f.fs.OpenFile(e.Name(), os.O_RDONLY)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", name, err)
		}
		return &File{r: r, size: info.Size()}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// File is a regular file opened read-only from a FAT filesystem.
type File struct {
	r    io.ReadCloser
	size int64
}

// Size returns the size of the file in bytes.
func (f *File) Size() int64 {
	return f.size
}

// Read reads file content.
func (f *File) Read(p []byte) (int, error) {
	return f.r.Read(p)
}

// Close closes the file.
func (f *File) Close() error {
	return f.r.Close()
}
