// Package inspect implements the inspect command.
package inspect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/takumin/libreelec-repacker/internal/libreelec"
)

// NewCommands returns the inspect command.
func NewCommands(flags []cli.Flag) *cli.Command {
	return &cli.Command{
		Name:      "inspect",
		Usage:     "inspect a LibreELEC disk image without modifying it",
		ArgsUsage: "<image>",
		Flags:     flags,
		Action:    action,
	}
}

func action(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 1 {
		return fmt.Errorf("inspect: expected exactly one image path, got %d arguments", cmd.Args().Len())
	}
	path := cmd.Args().First()

	insp, err := libreelec.Inspect(ctx, path)
	if insp != nil {
		if werr := write(cmd.Writer, path, insp); werr != nil {
			err = errors.Join(err, fmt.Errorf("write report: %w", werr))
		}
	}
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	return nil
}

// write prints the inspection as a human-readable report.
func write(w io.Writer, path string, insp *libreelec.Inspection) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	r := &report{w: tw}

	r.printf("Image:\t%s\n", path)
	r.printf("Format:\t%s\n", insp.Format)
	r.printf("Size:\t%d bytes\n", insp.Size)
	if insp.PartitionTable == nil {
		r.printf("Partition table:\tnone\n")
		return r.flush(tw)
	}
	r.printf("Partition table:\t%s\n", insp.PartitionTable.Type)

	r.printf("\nPartitions:\n")
	r.printf("  INDEX\tSTART\tSIZE\tFILESYSTEM\n")
	for _, p := range insp.PartitionTable.Partitions {
		r.printf("  %d\t%d\t%d\t%s\n", p.Index, p.Start, p.Size, p.Filesystem)
	}

	r.printf("\nLibreELEC:\n")
	if insp.Boot == nil {
		r.printf("  Boot partition:\tnot found\n")
		return r.flush(tw)
	}
	magic := "invalid"
	if insp.Boot.SystemSquashFS {
		magic = "valid"
	}
	r.printf("  Boot partition:\t%d\n", insp.Boot.PartitionIndex)
	r.printf("  %s size:\t%d bytes\n", libreelec.SystemFileName, insp.Boot.SystemSize)
	r.printf("  %s SquashFS magic:\t%s\n", libreelec.SystemFileName, magic)
	return r.flush(tw)
}

// report writes formatted lines and keeps the first write error.
type report struct {
	w   io.Writer
	err error
}

func (r *report) printf(format string, a ...any) {
	if r.err == nil {
		_, r.err = fmt.Fprintf(r.w, format, a...)
	}
}

func (r *report) flush(tw *tabwriter.Writer) error {
	if r.err != nil {
		return r.err
	}
	return tw.Flush()
}
