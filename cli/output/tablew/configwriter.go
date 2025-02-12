package tablew

import (
	"fmt"
	"io"

	"github.com/fatih/color"

	"github.com/samber/lo"

	"github.com/neilotoole/sq/libsq/core/options"

	"github.com/neilotoole/sq/cli/output"
)

var _ output.ConfigWriter = (*configWriter)(nil)

// configWriter implements output.ConfigWriter.
type configWriter struct {
	tbl *table
}

// NewConfigWriter returns a new output.ConfigWriter.
func NewConfigWriter(out io.Writer, pr *output.Printing) output.ConfigWriter {
	tbl := &table{out: out, pr: pr, header: pr.ShowHeader}
	tbl.reset()
	return &configWriter{tbl: tbl}
}

// Location implements output.ConfigWriter.
func (w *configWriter) Location(path, origin string) error {
	fmt.Fprintln(w.tbl.out, path)
	if w.tbl.pr.Verbose && origin != "" {
		w.tbl.pr.Faint.Fprint(w.tbl.out, "Origin: ")
		w.tbl.pr.String.Fprintln(w.tbl.out, origin)
	}

	return nil
}

// Opt implements output.ConfigWriter.
func (w *configWriter) Opt(o options.Options, opt options.Opt) error {
	if o == nil || opt == nil {
		return nil
	}

	if !w.tbl.pr.Verbose {
		if !o.IsSet(opt) {
			return nil
		}
		clr := getOptColor(w.tbl.pr, opt)
		clr.Fprintln(w.tbl.out, opt.GetAny(o))
		return nil
	}

	o2 := options.Options{opt.Key(): o[opt.Key()]}
	reg2 := &options.Registry{}
	reg2.Add(opt)
	return w.Options(reg2, o2)
}

// Options implements output.ConfigWriter.
func (w *configWriter) Options(reg *options.Registry, o options.Options) error {
	if o == nil {
		return nil
	}

	if w.tbl.pr.Verbose {
		w.tbl.pr.ShowHeader = true
	} else {
		w.tbl.pr.ShowHeader = false
	}

	w.doPrintOptions(reg, o, true)
	return nil
}

// Options implements output.ConfigWriter.
// If printUnset is true and we're in verbose mode, unset options
// are also printed.
func (w *configWriter) doPrintOptions(reg *options.Registry, o options.Options, printUnset bool) {
	if o == nil {
		return
	}

	t, pr, verbose := w.tbl.tblImpl, w.tbl.pr, w.tbl.pr.Verbose

	if pr.ShowHeader {
		headers := []string{"KEY", "VALUE"}
		if verbose {
			headers = []string{"KEY", "VALUE", "DEFAULT"}
		}

		t.SetHeader(headers)
	}
	t.SetColTrans(0, pr.Key.SprintFunc())

	var rows [][]string

	keys := o.Keys()
	for _, k := range keys {
		opt := reg.Get(k)
		if opt == nil {
			// Shouldn't happen, but print anyway just in case
			row := []string{
				k,
				pr.Error.Sprintf("%v", o[k]),
			}
			if verbose {
				// Can't know default in this situation.
				row = append(row, "")
			}

			rows = append(rows, row)
			continue
		}

		clr := getOptColor(pr, opt)

		val, ok := o[k]
		if !ok || val == nil {
			if verbose {
				val = ""
			} else {
				val = "NULL"
			}
			clr = pr.Null
		}

		row := []string{
			k,
			clr.Sprintf("%v", val),
		}

		if verbose {
			defaultVal := pr.Faint.Sprintf("%v", opt.GetAny(nil))
			row = append(row, defaultVal)
		}

		rows = append(rows, row)
	}

	if !printUnset || !verbose {
		w.tbl.appendRowsAndRenderAll(rows)
		return
	}

	// Also print the unset opts
	allKeys := reg.Keys()
	setKeys := o.Keys()
	unsetKeys, _ := lo.Difference(allKeys, setKeys)

	for _, k := range unsetKeys {
		opt := reg.Get(k)
		row := []string{
			pr.Faint.Sprintf("%v", k),
			"", // opt is unset, so it doesn't have a value
			pr.Faint.Sprintf("%v", opt.GetAny(nil)),
		}

		rows = append(rows, row)
	}

	w.tbl.appendRowsAndRenderAll(rows)
}

// SetOption implements output.ConfigWriter.
func (w *configWriter) SetOption(o options.Options, opt options.Opt) error {
	if !w.tbl.pr.Verbose {
		// No output unless verbose
		return nil
	}

	reg2 := &options.Registry{}
	reg2.Add(opt)
	o = options.Options{opt.Key(): opt.GetAny(o)}

	// It's verbose
	o = options.Effective(o, opt)
	w.tbl.pr.ShowHeader = true
	w.doPrintOptions(reg2, o, false)
	return nil
}

// UnsetOption implements output.ConfigWriter.
func (w *configWriter) UnsetOption(opt options.Opt) error {
	if !w.tbl.pr.Verbose {
		// No output unless verbose
		return nil
	}

	reg := &options.Registry{}
	reg.Add(opt)
	o := options.Options{}

	w.doPrintOptions(reg, o, true)
	return nil
}

func getOptColor(pr *output.Printing, opt options.Opt) *color.Color {
	if opt == nil {
		return pr.Null
	}

	clr := pr.String
	switch opt.(type) {
	case options.Bool:
		clr = pr.Bool
	case options.Int:
		clr = pr.Number
	default:
	}

	return clr
}
