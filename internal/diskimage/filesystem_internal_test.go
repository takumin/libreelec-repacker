package diskimage

import (
	"encoding/binary"
	"testing"
)

// bootSector returns a FAT16 boot sector of a 16 MiB volume, modified by fn.
func bootSector(fn func(b []byte)) []byte {
	b := make([]byte, 512)
	binary.LittleEndian.PutUint16(b[0x0B:], 512)   // bytes per sector
	b[0x0D] = 4                                    // sectors per cluster
	binary.LittleEndian.PutUint16(b[0x0E:], 4)     // reserved sectors
	b[0x10] = 2                                    // FAT count
	binary.LittleEndian.PutUint16(b[0x11:], 512)   // root directory entries
	binary.LittleEndian.PutUint16(b[0x13:], 32768) // total sectors
	b[0x15] = 0xF8                                 // media descriptor
	binary.LittleEndian.PutUint16(b[0x16:], 32)    // sectors per FAT
	if fn != nil {
		fn(b)
	}
	return b
}

func TestFATType(t *testing.T) {
	t.Parallel()

	const size = 16 << 20

	cases := map[string]struct {
		sector []byte
		size   int64
		want   FilesystemType
	}{
		"fat16": {bootSector(nil), size, FilesystemFAT16},
		"fat12": {bootSector(func(b []byte) {
			b[0x0D] = 64
		}), size, FilesystemFAT12},
		"fat32": {bootSector(func(b []byte) {
			binary.LittleEndian.PutUint16(b[0x11:], 0)
			binary.LittleEndian.PutUint16(b[0x16:], 0)
			binary.LittleEndian.PutUint32(b[0x24:], 64)
		}), size, FilesystemFAT32},
		"zeros":                    {make([]byte, 512), size, FilesystemUnknown},
		"short":                    {bootSector(nil)[:100], size, FilesystemUnknown},
		"zero-sectors-per-cluster": {bootSector(func(b []byte) { b[0x0D] = 0 }), size, FilesystemUnknown},
		"odd-sectors-per-cluster":  {bootSector(func(b []byte) { b[0x0D] = 3 }), size, FilesystemUnknown},
		"bad-bytes-per-sector":     {bootSector(func(b []byte) { binary.LittleEndian.PutUint16(b[0x0B:], 100) }), size, FilesystemUnknown},
		"no-reserved-sectors":      {bootSector(func(b []byte) { binary.LittleEndian.PutUint16(b[0x0E:], 0) }), size, FilesystemUnknown},
		"no-fats":                  {bootSector(func(b []byte) { b[0x10] = 0 }), size, FilesystemUnknown},
		"bad-media":                {bootSector(func(b []byte) { b[0x15] = 0x12 }), size, FilesystemUnknown},
		"larger-than-partition":    {bootSector(nil), size / 2, FilesystemUnknown},
		"fat-larger-than-volume": {bootSector(func(b []byte) {
			binary.LittleEndian.PutUint16(b[0x16:], 40000)
		}), size, FilesystemUnknown},
		"fat32-with-root-entries": {bootSector(func(b []byte) {
			binary.LittleEndian.PutUint16(b[0x16:], 0)
			binary.LittleEndian.PutUint32(b[0x24:], 64)
		}), size, FilesystemUnknown},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := fatType(tt.sector, tt.size); got != tt.want {
				t.Errorf("fatType() = %q, want %q", got, tt.want)
			}
		})
	}
}
