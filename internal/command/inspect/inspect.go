// Package inspect implements the inspect command.
package inspect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/takumin/libreelec-repacker/internal/libreelec"
)

// Output formats accepted by the --output flag.
const (
	OutputPretty = "pretty"
	OutputJSON   = "json"
)

// writers maps each output format to the function that renders it.
var writers = map[string]func(io.Writer, string, *libreelec.Inspection) error{
	OutputPretty: write,
	OutputJSON:   writeJSON,
}

// NewCommands returns the inspect command.
func NewCommands(flags []cli.Flag) *cli.Command {
	return &cli.Command{
		Name:      "inspect",
		Usage:     "inspect a LibreELEC disk image without modifying it",
		ArgsUsage: "<image>",
		Flags: append(slices.Clone(flags), &cli.StringFlag{
			Name:    "output",
			Aliases: []string{"o"},
			Usage:   "output format (" + OutputPretty + ", " + OutputJSON + ")",
			Value:   OutputPretty,
			Validator: func(s string) error {
				if _, ok := writers[s]; !ok {
					return fmt.Errorf("unknown output format: %s", s)
				}
				return nil
			},
		}),
		Action: action,
	}
}

func action(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 1 {
		return fmt.Errorf("inspect: expected exactly one image path, got %d arguments", cmd.Args().Len())
	}
	path := cmd.Args().First()
	render := writers[cmd.String("output")]

	insp, err := libreelec.Inspect(ctx, path)
	if insp != nil {
		if werr := render(cmd.Writer, path, insp); werr != nil {
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

// jsonInspection is the JSON representation of an inspection. It is kept
// separate from libreelec.Inspection so that the output schema does not change
// with internal refactoring.
type jsonInspection struct {
	Image          string              `json:"image"`
	Format         string              `json:"format"`
	Size           int64               `json:"size"`
	PartitionTable *jsonPartitionTable `json:"partition_table"`
	Boot           *jsonBoot           `json:"boot"`
}

type jsonPartitionTable struct {
	Type       string          `json:"type"`
	Partitions []jsonPartition `json:"partitions"`
}

type jsonPartition struct {
	Index      int    `json:"index"`
	Start      int64  `json:"start"`
	Size       int64  `json:"size"`
	Filesystem string `json:"filesystem"`
}

type jsonBoot struct {
	PartitionIndex int   `json:"partition_index"`
	SystemSize     int64 `json:"system_size"`
	SystemSquashFS bool  `json:"system_squashfs"`
}

// writeJSON prints the inspection as an indented JSON document. A missing
// partition table or boot filesystem is written as null.
func writeJSON(w io.Writer, path string, insp *libreelec.Inspection) error {
	out := jsonInspection{
		Image:  path,
		Format: string(insp.Format),
		Size:   insp.Size,
	}
	if pt := insp.PartitionTable; pt != nil {
		out.PartitionTable = &jsonPartitionTable{
			Type:       string(pt.Type),
			Partitions: make([]jsonPartition, 0, len(pt.Partitions)),
		}
		for _, p := range pt.Partitions {
			out.PartitionTable.Partitions = append(out.PartitionTable.Partitions, jsonPartition{
				Index:      p.Index,
				Start:      p.Start,
				Size:       p.Size,
				Filesystem: string(p.Filesystem),
			})
		}
	}
	if b := insp.Boot; b != nil {
		out.Boot = &jsonBoot{
			PartitionIndex: b.PartitionIndex,
			SystemSize:     b.SystemSize,
			SystemSquashFS: b.SystemSquashFS,
		}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
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
