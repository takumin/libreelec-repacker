# libreelec-repacker
Declaratively customize and repack official LibreELEC disk images without rebuilding from source.

[![CI](https://github.com/takumin/libreelec-repacker/actions/workflows/integration.yml/badge.svg)](https://github.com/takumin/libreelec-repacker/actions/workflows/integration.yml)
[![Coverage](https://raw.githubusercontent.com/takumin/octocov-central/main/badges/takumin/libreelec-repacker/coverage.svg)](https://github.com/takumin/libreelec-repacker/actions/workflows/integration.yml)
[![Code to Test Ratio](https://raw.githubusercontent.com/takumin/octocov-central/main/badges/takumin/libreelec-repacker/ratio.svg)](https://github.com/takumin/libreelec-repacker/actions/workflows/integration.yml)
[![Test Execution Time](https://raw.githubusercontent.com/takumin/octocov-central/main/badges/takumin/libreelec-repacker/time.svg)](https://github.com/takumin/libreelec-repacker/actions/workflows/integration.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/takumin/libreelec-repacker)](https://goreportcard.com/report/github.com/takumin/libreelec-repacker)

## Usage

Only read-only inspection is implemented so far.

### inspect

`inspect` reads a local LibreELEC disk image, either raw (`.img`) or gzip-compressed (`.img.gz`), and reports its partition layout and the LibreELEC boot filesystem.
It does not mount anything, does not need root privileges and does not modify the image.
Gzip-compressed images are decompressed into a temporary file (in `$TMPDIR`), which is removed afterwards.

Example output (offsets and sizes vary by image):

```console
$ libreelec-repacker inspect LibreELEC.img.gz
Image:            LibreELEC.img.gz
Format:           gzip
Size:             575668224 bytes
Partition table:  mbr

Partitions:
  INDEX  START      SIZE       FILESYSTEM
  1      4194304    536870912  fat16
  2      541065216  33554432   ext2/3/4

LibreELEC:
  Boot partition:         1
  SYSTEM size:            287358976 bytes
  SYSTEM SquashFS magic:  valid
```

The boot partition is the FAT filesystem whose root directory contains a `SYSTEM` file, and `SYSTEM` must start with the SquashFS magic.
If the image is not a valid LibreELEC image, `inspect` prints what it found and exits with a non-zero status.

`--output` (`-o`) selects the output format: `pretty` (default) or `json`.
In JSON, sizes and offsets are in bytes, and `partition_table` or `boot` is `null` when it was not found:

```console
$ libreelec-repacker inspect --output json LibreELEC.img.gz
{
  "image": "LibreELEC.img.gz",
  "format": "gzip",
  "size": 575668224,
  "partition_table": {
    "type": "mbr",
    "partitions": [
      {
        "index": 1,
        "start": 4194304,
        "size": 536870912,
        "filesystem": "fat16"
      },
      {
        "index": 2,
        "start": 541065216,
        "size": 33554432,
        "filesystem": "ext2/3/4"
      }
    ]
  },
  "boot": {
    "partition_index": 1,
    "system_size": 287358976,
    "system_squashfs": true
  }
}
```
