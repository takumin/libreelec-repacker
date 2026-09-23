// Package diskimage provides strictly read-only access to raw disk images.
//
// It detects compressed input, reads the partition table and exposes the
// FAT filesystems stored in partitions without mounting anything and without
// requiring elevated privileges. It knows nothing about LibreELEC.
package diskimage

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/backend/file"
	"github.com/diskfs/go-diskfs/disk"
)

// Format describes how the disk image is stored on the local filesystem.
type Format string

const (
	// FormatRaw is an uncompressed disk image.
	FormatRaw Format = "raw"
	// FormatGzip is a gzip-compressed disk image.
	FormatGzip Format = "gzip"
)

// gzipMagic is the signature every gzip member starts with (RFC 1952).
var gzipMagic = []byte{0x1f, 0x8b}

// Image is a disk image opened read-only.
//
// An Image must be closed with Close, which also removes the temporary raw
// image created for compressed input.
type Image struct {
	format   Format
	disk     *disk.Disk
	tempPath string
}

// Open opens the local disk image at path read-only.
//
// Compression is detected from the file signature, not from the file name.
// Gzip-compressed input is decompressed into a temporary raw image in the
// default temporary directory (see os.TempDir). The temporary image is
// removed if Open fails, and by Close otherwise. Cancelling ctx aborts the
// decompression.
func Open(ctx context.Context, path string) (*Image, error) {
	format, err := detectFormat(path)
	if err != nil {
		return nil, err
	}

	img := &Image{format: format}
	rawPath := path
	if format == FormatGzip {
		img.tempPath, err = decompressGzip(ctx, path)
		if err != nil {
			return nil, err
		}
		rawPath = img.tempPath
	}

	img.disk, err = openDisk(rawPath)
	if err != nil {
		return nil, errors.Join(err, img.removeTemp())
	}
	return img, nil
}

// Format returns the storage format of the input file.
func (img *Image) Format() Format {
	return img.format
}

// Size returns the size of the raw (uncompressed) disk image in bytes.
func (img *Image) Size() int64 {
	return img.disk.Size
}

// Close releases the image and removes any temporary raw image.
func (img *Image) Close() error {
	var errs []error
	if img.disk != nil {
		if err := img.disk.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close disk image: %w", err))
		}
		img.disk = nil
	}
	errs = append(errs, img.removeTemp())
	return errors.Join(errs...)
}

func (img *Image) removeTemp() error {
	if img.tempPath == "" {
		return nil
	}
	if err := os.Remove(img.tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove temporary image %s: %w", img.tempPath, err)
	}
	img.tempPath = ""
	return nil
}

func detectFormat(path string) (format Format, err error) {
	f, err := os.Open(path) // #nosec G304 -- the user chooses which image to inspect
	if err != nil {
		return "", fmt.Errorf("open image: %w", err)
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()

	magic := make([]byte, len(gzipMagic))
	n, err := io.ReadFull(f, magic)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("read image signature: %w", err)
	}
	if bytes.Equal(magic[:n], gzipMagic) {
		return FormatGzip, nil
	}
	return FormatRaw, nil
}

func decompressGzip(ctx context.Context, path string) (tempPath string, err error) {
	src, err := os.Open(path) // #nosec G304 -- the user chooses which image to inspect
	if err != nil {
		return "", fmt.Errorf("open image: %w", err)
	}
	// The source is only read, so closing it cannot lose data. Its error is
	// ignored so that it can never be reported after the temporary image has
	// been kept.
	defer func() { _ = src.Close() }()

	zr, err := gzip.NewReader(src)
	if err != nil {
		return "", fmt.Errorf("read gzip header: %w", err)
	}

	dst, err := os.CreateTemp("", "libreelec-repacker-*.img")
	if err != nil {
		return "", fmt.Errorf("create temporary image: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(dst.Name()))
		}
	}()

	if _, err := io.Copy(dst, &contextReader{ctx: ctx, r: zr}); err != nil {
		return "", errors.Join(fmt.Errorf("decompress gzip image: %w", err), dst.Close())
	}
	if err := dst.Close(); err != nil {
		return "", fmt.Errorf("close temporary image: %w", err)
	}
	return dst.Name(), nil
}

// contextReader stops reading once its context is cancelled.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func openDisk(path string) (*disk.Disk, error) {
	f, err := os.Open(path) // #nosec G304 -- the user chooses which image to inspect
	if err != nil {
		return nil, fmt.Errorf("open image: %w", err)
	}
	d, err := diskfs.OpenBackend(
		file.New(f, true),
		diskfs.WithOpenMode(diskfs.ReadOnly),
		diskfs.WithSectorSize(diskfs.SectorSize512),
	)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open disk image: %w", err), f.Close())
	}
	return d, nil
}
