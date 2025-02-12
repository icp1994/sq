package yamlw

import (
	"io"
	"slices"

	"github.com/goccy/go-yaml/printer"

	"github.com/neilotoole/sq/cli/output"
	"github.com/neilotoole/sq/libsq/source"
)

var _ output.SourceWriter = (*sourceWriter)(nil)

type sourceWriter struct {
	p   printer.Printer
	out io.Writer
	pr  *output.Printing
}

// NewSourceWriter returns a source writer that outputs source
// details in text table format.
func NewSourceWriter(out io.Writer, pr *output.Printing) output.SourceWriter {
	return &sourceWriter{out: out, pr: pr, p: newPrinter(pr)}
}

// Collection implements output.SourceWriter.
func (w *sourceWriter) Collection(coll *source.Collection) error {
	if coll == nil {
		return nil
	}

	// This is a bit hacky. Basically we want to YAML-print coll.Data().
	// But, we want to do it just for the active group.
	// So, our hack is that we clone the coll, and remove any
	// sources that are not in the active group.
	//
	// This whole function, including what it outputs, should be revisited.
	coll = coll.Clone()
	group := coll.ActiveGroup()

	// We store the active src handle
	activeHandle := coll.ActiveHandle()

	handles, err := coll.HandlesInGroup(group)
	if err != nil {
		return err
	}

	srcs := coll.Sources()
	for _, src := range srcs {
		if !slices.Contains(handles, src.Handle) {
			if err = coll.Remove(src.Handle); err != nil {
				// Should never happen
				return err
			}
		}
	}

	srcs = coll.Sources()
	for i := range srcs {
		srcs[i].Location = srcs[i].RedactedLocation()
	}

	// HACK: we set the activeHandle back, even though that
	// active source may have been removed (because it is not in
	// the active group). This whole thing is a mess.
	_, _ = coll.SetActive(activeHandle, true)

	return writeYAML(w.out, w.p, coll.Data())
}

// Source implements output.SourceWriter.
func (w *sourceWriter) Source(_ *source.Collection, src *source.Source) error {
	if src == nil {
		return nil
	}

	src = src.Clone()
	src.Location = src.RedactedLocation()
	return writeYAML(w.out, w.p, src)
}

// Removed implements output.SourceWriter.
func (w *sourceWriter) Removed(srcs ...*source.Source) error {
	if !w.pr.Verbose || len(srcs) == 0 {
		return nil
	}

	srcs2 := make([]*source.Source, len(srcs))
	for i := range srcs {
		srcs2[i] = srcs[i].Clone()
		srcs2[i].Location = srcs2[i].RedactedLocation()
	}
	return writeYAML(w.out, w.p, srcs2)
}

// Group implements output.SourceWriter.
func (w *sourceWriter) Group(group *source.Group) error {
	if group == nil {
		return nil
	}

	source.RedactGroup(group)
	return writeYAML(w.out, w.p, group)
}

// SetActiveGroup implements output.SourceWriter.
func (w *sourceWriter) SetActiveGroup(group *source.Group) error {
	if group == nil {
		return nil
	}

	source.RedactGroup(group)
	return writeYAML(w.out, w.p, group)
}

// Groups implements output.SourceWriter.
func (w *sourceWriter) Groups(tree *source.Group) error {
	source.RedactGroup(tree)
	return writeYAML(w.out, w.p, tree)
}
