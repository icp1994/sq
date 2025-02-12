package cli

import (
	"context"

	"github.com/neilotoole/sq/cli/run"

	"github.com/neilotoole/sq/cli/flag"
	"github.com/neilotoole/sq/libsq/core/errz"
	"github.com/neilotoole/sq/libsq/core/lg"
	"github.com/neilotoole/sq/libsq/core/lg/lga"
	"github.com/neilotoole/sq/libsq/core/options"
	"github.com/neilotoole/sq/libsq/driver"
	"github.com/neilotoole/sq/libsq/source"

	"github.com/spf13/cobra"
)

// determineSources figures out what the active source is
// from any combination of stdin, flags or cfg. It will
// mutate ru.Config.Collection as necessary. If no error
// is returned, it is guaranteed that there's an active
// source on the collection.
func determineSources(ctx context.Context, ru *run.Run) error {
	cmd, coll := ru.Cmd, ru.Config.Collection
	activeSrc, err := activeSrcFromFlagsOrConfig(cmd, coll)
	if err != nil {
		return err
	}
	// Note: ^ activeSrc could still be nil

	// check if there's input on stdin
	stdinSrc, err := checkStdinSource(ctx, ru)
	if err != nil {
		return err
	}

	if stdinSrc != nil {
		// We have a valid source on stdin.

		// Add the stdin source to coll.
		err = coll.Add(stdinSrc)
		if err != nil {
			return err
		}

		if !cmdFlagChanged(cmd, flag.ActiveSrc) {
			// If the user has not explicitly set an active
			// source via flag, then we set the stdin pipe data
			// source as the active source.
			// We do this because the @stdin src is commonly the
			// only data source the user cares about in a pipe
			// situation.
			_, err = coll.SetActive(stdinSrc.Handle, false)
			if err != nil {
				return err
			}
			activeSrc = stdinSrc
		}
	}

	if activeSrc == nil {
		return errz.New(msgNoActiveSrc)
	}

	return nil
}

// activeSrcFromFlagsOrConfig gets the active source, either
// from flagActiveSrc or from srcs.Active. An error is returned
// if the flag src is not found: if the flag src is found,
// it is set as the active src on coll. If the flag was not
// set and there is no active src in coll, (nil, nil) is
// returned.
func activeSrcFromFlagsOrConfig(cmd *cobra.Command, coll *source.Collection) (*source.Source, error) {
	var activeSrc *source.Source

	if cmdFlagChanged(cmd, flag.ActiveSrc) {
		// The user explicitly wants to set an active source
		// just for this query.

		handle, _ := cmd.Flags().GetString(flag.ActiveSrc)
		s, err := coll.Get(handle)
		if err != nil {
			return nil, errz.Wrapf(err, "flag --%s", flag.ActiveSrc)
		}

		activeSrc, err = coll.SetActive(s.Handle, false)
		if err != nil {
			return nil, err
		}
	} else {
		activeSrc = coll.Active()
	}
	return activeSrc, nil
}

// checkStdinSource checks if there's stdin data (on pipe/redirect).
// If there is, that pipe is inspected, and if it has recognizable
// input, a new source instance with handle @stdin is constructed
// and returned. If the pipe has no data (size is zero),
// then (nil,nil) is returned.
func checkStdinSource(ctx context.Context, ru *run.Run) (*source.Source, error) {
	cmd := ru.Cmd

	f := ru.Stdin
	info, err := f.Stat()
	if err != nil {
		return nil, errz.Wrap(err, "failed to get stat on stdin")
	}

	if info.Size() <= 0 {
		// Doesn't make sense to have zero-data pipe? just ignore.
		return nil, nil //nolint:nilnil
	}

	// If we got this far, we have pipe input

	typ := source.TypeNone
	if cmd.Flags().Changed(flag.IngestDriver) {
		val, _ := cmd.Flags().GetString(flag.IngestDriver)
		typ = source.DriverType(val)
		if ru.DriverRegistry.ProviderFor(typ) == nil {
			return nil, errz.Errorf("unknown driver type: %s", typ)
		}
	}

	err = ru.Files.AddStdin(f)
	if err != nil {
		return nil, err
	}

	if typ == source.TypeNone {
		typ, err = ru.Files.TypeStdin(ctx)
		if err != nil {
			return nil, err
		}
		if typ == source.TypeNone {
			return nil, errz.New("unable to detect type of stdin: use flag --driver")
		}
	}

	return newSource(
		ctx,
		ru.DriverRegistry,
		typ,
		source.StdinHandle,
		source.StdinHandle,
		options.Options{},
	)
}

// newSource creates a new Source instance where the
// driver type is known. Opts may be nil.
func newSource(ctx context.Context, dp driver.Provider, typ source.DriverType, handle, loc string,
	opts options.Options,
) (*source.Source, error) {
	log := lg.FromContext(ctx)

	if opts == nil {
		log.Debug("Create new data source",
			lga.Handle, handle,
			lga.Driver, typ,
			lga.Loc, source.RedactLocation(loc),
		)
	} else {
		log.Debug("Create new data source with opts",
			lga.Handle, handle,
			lga.Driver, typ,
			lga.Loc, source.RedactLocation(loc),
			// lga.Opts, opts.Encode(), // FIXME: encode opts for debugging
		)
	}

	err := source.ValidHandle(handle)
	if err != nil {
		return nil, err
	}

	drvr, err := dp.DriverFor(typ)
	if err != nil {
		return nil, err
	}

	src := &source.Source{Handle: handle, Location: loc, Type: typ, Options: opts}

	log.Debug("Validating provisional new data source", lga.Src, src)
	canonicalSrc, err := drvr.ValidateSource(src)
	if err != nil {
		return nil, err
	}
	return canonicalSrc, nil
}
