package diskimage

import (
	"errors"
	"fmt"

	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/diskfs/go-diskfs/partition/mbr"
)

// ErrNoPartitionTable is returned when the image has no recognizable MBR or
// GPT partition table.
var ErrNoPartitionTable = errors.New("no partition table found")

// TableType identifies the partitioning scheme of a disk image.
type TableType string

const (
	// TableMBR is a DOS/MBR partition table.
	TableMBR TableType = "mbr"
	// TableGPT is a GUID partition table.
	TableGPT TableType = "gpt"
)

// PartitionTable is the partition layout of a disk image.
type PartitionTable struct {
	Type TableType
	// Partitions lists the used entries in table order.
	Partitions []Partition
}

// Partition is a used entry of a partition table.
type Partition struct {
	// Index is the 1-based position of the entry in the partition table.
	Index int
	// Start is the byte offset of the partition from the start of the image.
	Start int64
	// Size is the size of the partition in bytes.
	Size int64
	// Filesystem is the filesystem detected from the partition's on-disk
	// signature, or FilesystemUnknown.
	Filesystem FilesystemType
}

// PartitionTable reads the partition table of the image and detects the
// filesystem of every used partition.
//
// It returns an error wrapping ErrNoPartitionTable when the image has no MBR
// or GPT partition table, and an error when a partition does not fit inside
// the image.
func (img *Image) PartitionTable() (*PartitionTable, error) {
	table, err := img.disk.GetPartitionTable()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoPartitionTable, err)
	}

	result := &PartitionTable{}
	switch t := table.(type) {
	case *mbr.Table:
		result.Type = TableMBR
		for _, p := range t.Partitions {
			if p.Type == mbr.Empty || p.Size == 0 {
				continue
			}
			result.Partitions = append(result.Partitions, Partition{
				Index: p.GetIndex(),
				Start: p.GetStart(),
				Size:  p.GetSize(),
			})
		}
	case *gpt.Table:
		result.Type = TableGPT
		for _, p := range t.Partitions {
			result.Partitions = append(result.Partitions, Partition{
				Index: p.GetIndex(),
				Start: p.GetStart(),
				Size:  p.GetSize(),
			})
		}
	default:
		return nil, fmt.Errorf("unsupported partition table type %q", table.Type())
	}

	for i := range result.Partitions {
		p := &result.Partitions[i]
		if p.Start < 0 || p.Size <= 0 || p.Start > img.Size()-p.Size {
			return nil, fmt.Errorf(
				"partition %d (start %d, size %d) does not fit in image of size %d",
				p.Index, p.Start, p.Size, img.Size(),
			)
		}
		p.Filesystem, err = img.detectFilesystem(p.Start, p.Size)
		if err != nil {
			return nil, fmt.Errorf("partition %d: %w", p.Index, err)
		}
	}
	return result, nil
}
