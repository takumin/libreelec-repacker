// Package libreelec discovers and validates the layout of LibreELEC disk
// images.
package libreelec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/takumin/libreelec-repacker/internal/diskimage"
)

// SystemFileName is the name of the SquashFS root filesystem image stored in
// the root directory of the LibreELEC boot filesystem.
const SystemFileName = "SYSTEM"

// squashfsMagic is the little-endian SquashFS superblock magic ("hsqs").
var squashfsMagic = []byte("hsqs")

var (
	// ErrBootNotFound is returned when no FAT filesystem in the image
	// contains a SYSTEM file.
	ErrBootNotFound = errors.New("no FAT filesystem containing " + SystemFileName + " found")
	// ErrMultipleBoot is returned when more than one FAT filesystem contains
	// a SYSTEM file, so the boot filesystem cannot be identified.
	ErrMultipleBoot = errors.New("multiple FAT filesystems contain " + SystemFileName)
	// ErrInvalidSystem is returned when the SYSTEM file is not a SquashFS
	// image.
	ErrInvalidSystem = errors.New(SystemFileName + " does not start with the SquashFS magic")
)

// Inspection is what Inspect found in a disk image.
type Inspection struct {
	// Format is the storage format of the input file.
	Format diskimage.Format
	// Size is the size of the raw (uncompressed) disk image in bytes.
	Size int64
	// PartitionTable is the partition layout, or nil when the image has no
	// readable partition table.
	PartitionTable *diskimage.PartitionTable
	// Boot is the LibreELEC boot filesystem, or nil when it was not found.
	Boot *Boot
}

// Boot describes the FAT filesystem that holds the LibreELEC SYSTEM file.
type Boot struct {
	// PartitionIndex is the partition table index of the boot filesystem.
	PartitionIndex int
	// SystemSize is the size of the SYSTEM file in bytes.
	SystemSize int64
	// SystemSquashFS reports whether SYSTEM starts with the SquashFS magic.
	SystemSquashFS bool
}

// Inspect opens the disk image at path read-only and locates the LibreELEC
// boot filesystem and its SYSTEM file.
//
// The boot filesystem is identified as the only FAT filesystem whose root
// directory contains a SYSTEM file, regardless of its partition index.
//
// Inspect returns a nil error only for a valid LibreELEC image. When the image
// can be opened but is not a valid LibreELEC image, Inspect returns what it
// found so far together with an error, which wraps
// diskimage.ErrNoPartitionTable, ErrBootNotFound, ErrMultipleBoot or
// ErrInvalidSystem where applicable.
func Inspect(ctx context.Context, path string) (insp *Inspection, err error) {
	img, err := diskimage.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, img.Close())
	}()

	insp = &Inspection{Format: img.Format(), Size: img.Size()}

	insp.PartitionTable, err = img.PartitionTable()
	if err != nil {
		return insp, err
	}

	var candidates []*Boot
	for _, p := range insp.PartitionTable.Partitions {
		boot, err := inspectBoot(img, p)
		if err != nil {
			return insp, err
		}
		if boot != nil {
			candidates = append(candidates, boot)
		}
	}

	switch len(candidates) {
	case 0:
		return insp, ErrBootNotFound
	case 1:
	default:
		indexes := make([]int, len(candidates))
		for i, c := range candidates {
			indexes[i] = c.PartitionIndex
		}
		return insp, fmt.Errorf("%w: partitions %v", ErrMultipleBoot, indexes)
	}

	insp.Boot = candidates[0]
	if !insp.Boot.SystemSquashFS {
		return insp, fmt.Errorf("partition %d: %w", insp.Boot.PartitionIndex, ErrInvalidSystem)
	}
	return insp, nil
}

// inspectBoot returns the boot filesystem description when partition p is a
// FAT filesystem containing SYSTEM, and nil when it is not.
func inspectBoot(img *diskimage.Image, p diskimage.Partition) (boot *Boot, err error) {
	fat, err := img.OpenFAT(p)
	if errors.Is(err, diskimage.ErrNotFAT) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	system, err := fat.Open(SystemFileName)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("partition %d: %w", p.Index, err)
	}
	defer func() {
		err = errors.Join(err, system.Close())
	}()

	magic := make([]byte, len(squashfsMagic))
	n, err := io.ReadFull(system, magic)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("partition %d: read %s: %w", p.Index, SystemFileName, err)
	}

	return &Boot{
		PartitionIndex: p.Index,
		SystemSize:     system.Size(),
		SystemSquashFS: bytes.Equal(magic[:n], squashfsMagic),
	}, nil
}
