package sakila_test

import (
	"testing"

	"github.com/neilotoole/sq/testh/tutil"

	"github.com/stretchr/testify/require"

	"github.com/neilotoole/sq/testh"
	"github.com/neilotoole/sq/testh/sakila"
)

// TestSakila_SQL is a sanity check for Sakila SQL test sources.
func TestSakila_SQL(t *testing.T) { //nolint:tparallel
	// Verify that the latest-version aliases are as expected
	require.Equal(t, sakila.Pg, sakila.Pg12)
	require.Equal(t, sakila.My, sakila.My8)
	require.Equal(t, sakila.MS, sakila.MS19)

	handles := sakila.SQLAll()
	for _, handle := range handles {
		handle := handle
		t.Run(handle, func(t *testing.T) {
			t.Parallel()

			th := testh.New(t)
			src := th.Source(handle)
			sink, err := th.QuerySQL(src, "SELECT * FROM actor")
			require.NoError(t, err)
			require.Equal(t, sakila.TblActorCount, len(sink.Recs))
		})
	}
}

// TestSakila_XLSX is a sanity check for Sakila XLSX test sources.
func TestSakila_XLSX(t *testing.T) {
	tutil.SkipWindows(t, "XLSX fails on windows pipeline (too slow)")

	handles := []string{sakila.XLSXSubset}
	// TODO: Add sakila.XLSX to handles when performance is reasonable
	//  enough not to break CI.

	for _, handle := range handles {
		handle := handle

		t.Run(handle, func(t *testing.T) {
			th := testh.New(t)
			src := th.Source(handle)

			sink, err := th.QuerySQL(src, "SELECT * FROM actor")
			require.NoError(t, err)
			require.Equal(t, sakila.TblActorCount, len(sink.Recs))
		})
	}
}

// TestSakila_CSV is a sanity check for Sakila CSV/TSV test sources.
func TestSakila_CSV(t *testing.T) {
	t.Parallel()

	handles := []string{sakila.CSVActor, sakila.CSVActorNoHeader, sakila.TSVActor, sakila.TSVActorNoHeader}
	for _, handle := range handles {
		handle := handle
		t.Run(handle, func(t *testing.T) {
			t.Parallel()

			th := testh.New(t)
			src := th.Source(handle)
			// Note table "data" instead of "actor", because CSV is monotable
			sink, err := th.QuerySQL(src, "SELECT * FROM data")
			require.NoError(t, err)
			require.Equal(t, sakila.TblActorCount, len(sink.Recs))
		})
	}
}

func TestSQLiteCloseError(t *testing.T) {
	th := testh.New(t)
	src := th.Source(sakila.CSVActor)
	// Note table "data" instead of "actor", because CSV is monotable
	sink, err := th.QuerySQL(src, "SELECT * FROM data")
	require.NoError(t, err)
	require.Equal(t, sakila.TblActorCount, len(sink.Recs))
}
