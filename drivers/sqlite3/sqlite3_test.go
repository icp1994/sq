package sqlite3_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/neilotoole/sq/libsq/core/kind"

	_ "github.com/mattn/go-sqlite3"
	"github.com/neilotoole/sq/testh/tutil"
	"github.com/stretchr/testify/require"

	"github.com/neilotoole/sq/drivers/sqlite3"
	"github.com/neilotoole/sq/libsq/core/sqlmodel"
	"github.com/neilotoole/sq/libsq/core/sqlz"
	"github.com/neilotoole/sq/libsq/core/stringz"
	"github.com/neilotoole/sq/libsq/source"
	"github.com/neilotoole/sq/testh"
	"github.com/neilotoole/sq/testh/fixt"
	"github.com/neilotoole/sq/testh/sakila"
)

func TestSmoke(t *testing.T) {
	t.Parallel()

	th := testh.New(t)
	src := th.Source(sakila.SL3)

	sink, err := th.QuerySQL(src, "SELECT * FROM "+sakila.TblFilm)
	require.NoError(t, err)
	require.Equal(t, sakila.TblFilmCount, len(sink.Recs))
}

func TestQueryEmptyTable(t *testing.T) {
	t.Parallel()

	th := testh.New(t)
	src := th.Source(sakila.SL3)

	// Get an empty table by copying an existing one
	tblName := th.CopyTable(true, src, sakila.TblFilm, "", false)
	require.Equal(t, int64(0), th.RowCount(src, tblName))

	sink, err := th.QuerySQL(src, "SELECT * FROM "+tblName)
	require.NoError(t, err)
	require.Equal(t, 0, len(sink.Recs))
}

// TestExhibitDriverColumnTypesBehavior shows the unusual
// behavior of SQLite wrt column types. The following is observed:
//
//  1. If rows.ColumnTypes is invoked prior to rows.Next being
//     invoked, the column ScanType will be nil.
//
//     UPDATE: ^^ As of mattn/go-sqlite3@v1.14.16 (and probably earlier)
//     this behavior seems to have changed.
//
//  2. The values returned by rows.ColumnTypes can change after
//     each call to rows.Next. This is because of SQLite's dynamic
//     typing: any value can be stored in any column.
//
// The second fact is potentially problematic for sq, as sq expects
// that the values of a column are each of the same type. Thus, sq
// will likely encounter problems dealing with SQLite tables
// that have mixed data types in columns.
func TestExhibitDriverColumnTypesBehavior(t *testing.T) {
	t.Parallel()

	th, src, _, _, db := testh.NewWith(t, sakila.SL3)
	t.Log("using source: " + src.Location)

	tblName := stringz.UniqTableName("scan_test")
	createStmt := "CREATE TABLE " + tblName + " (col1 REAL)"
	insertStmt := "INSERT INTO " + tblName + " VALUES(?)"
	query := "SELECT * FROM " + tblName

	// Create the table
	th.ExecSQL(src, createStmt)
	t.Cleanup(func() { th.DropTable(src, tblName) })

	// 1. Demonstrate that ColumnType.ScanType now correctly returns
	//    a valid value when rows.ColumnTypes is invoked prior to the first
	//    invocation of rows.Next. In earlier versions of the driver,
	//    it was necessary to invoke rows.Next first.
	rows1, err := db.Query(query)
	require.NoError(t, err)
	defer rows1.Close()

	colTypes, err := rows1.ColumnTypes()
	require.NoError(t, err)
	require.Equal(t, colTypes[0].Name(), "col1")
	scanType := colTypes[0].ScanType()
	require.Equal(t, sqlz.RTypeNullFloat64, scanType)

	require.False(t, rows1.Next()) // no rows yet since table is empty
	colTypes, err = rows1.ColumnTypes()
	require.Error(t, err, "ColumnTypes returns an error because the Next call closed rows")
	require.Nil(t, colTypes)

	// 2. In earlier versions of mattn/sqlite3, a column's scan type
	//    could be different for each row. Now, it seems that the
	//    returned scan type is the type of the column in the first
	//    non-null row. This does result in some weirdness: the reported
	//    scan type could be sql.NullFloat64, but passing *interface{} to
	//    row.Scan could result the variable being populated
	//    with, e.g. a string. Such is the SQLite life.

	// Insert values of various types
	_, err = db.Exec(insertStmt, nil)
	require.NoError(t, err)
	_, err = db.Exec(insertStmt, fixt.Float)
	require.NoError(t, err)
	_, err = db.Exec(insertStmt, fixt.Text)
	require.NoError(t, err)

	rows2, err := db.Query(query)
	require.NoError(t, err)
	defer rows2.Close()
	colTypes, err = rows2.ColumnTypes()
	require.NoError(t, err)
	scanType = colTypes[0].ScanType()
	require.Equal(t, sqlz.RTypeNullFloat64, scanType)

	var got any

	// 1st data row (nil)
	require.True(t, rows2.Next())
	require.NoError(t, rows2.Scan(&got))
	require.True(t, nil == got)
	colTypes, err = rows2.ColumnTypes()
	require.NoError(t, err)
	scanType = colTypes[0].ScanType()
	t.Logf("%s", scanType)
	require.Equal(t, sqlz.RTypeNullFloat64, scanType)

	// 2nd data row (float64)
	require.True(t, rows2.Next())
	require.NoError(t, rows2.Scan(&got))
	_, ok := got.(float64)
	require.True(t, ok)
	colTypes, err = rows2.ColumnTypes()
	require.NoError(t, err)
	scanType = colTypes[0].ScanType()
	t.Logf("%s", scanType)
	require.Equal(t, sqlz.RTypeNullFloat64.String(), scanType.String())

	// 3nd data row (string)
	require.True(t, rows2.Next())
	require.NoError(t, rows2.Scan(&got))

	_, ok = got.(string)
	require.True(t, ok, "a string was returned to us")
	colTypes, err = rows2.ColumnTypes()
	require.NoError(t, err)
	scanType = colTypes[0].ScanType()
	t.Log(scanType.String())
	require.NotNil(t, scanType, "scan type should be non-nil because the value is not nil")
	require.Equal(t, sqlz.RTypeNullFloat64.String(), scanType.String(),
		"the scan type is still sql.NullFloat64, even though the returned value is string")

	require.False(t, rows2.Next(), "should be end of rows")
	require.Nil(t, rows2.Err())
}

func TestDriver_CreateTable_NotNullDefault(t *testing.T) {
	t.Parallel()

	th, src, drvr, _, db := testh.NewWith(t, sakila.SL3)

	tblName := stringz.UniqTableName(t.Name())
	colNames, colKinds := fixt.ColNamePerKind(drvr.Dialect().IntBool, false, false)

	tblDef := sqlmodel.NewTableDef(tblName, colNames, colKinds)
	for _, colDef := range tblDef.Cols {
		colDef.NotNull = true
		colDef.HasDefault = true
	}

	err := drvr.CreateTable(th.Context, db, tblDef)
	require.NoError(t, err)
	t.Cleanup(func() { th.DropTable(src, tblName) })

	th.InsertDefaultRow(src, tblName)

	sink, err := th.QuerySQL(src, "SELECT * FROM "+tblName)
	require.NoError(t, err)
	require.Equal(t, 1, len(sink.Recs))
	require.Equal(t, len(colNames), len(sink.RecMeta))
	for i := range sink.Recs[0] {
		require.NotNil(t, sink.Recs[0][i])
	}
}

func TestPathFromLocation(t *testing.T) {
	testCases := []struct {
		loc     string
		want    string
		wantErr bool
	}{
		{loc: "sqlite3:///test.db", want: "/test.db"},
		{loc: "postgres:///test.db", wantErr: true},
		{loc: `sqlite3://C:\dir\sakila.db`, want: `C:\dir\sakila.db`},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.loc, func(t *testing.T) {
			src := &source.Source{
				Handle:   "@h1",
				Type:     sqlite3.Type,
				Location: tc.loc,
			}

			got, err := sqlite3.PathFromLocation(src)
			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			want := filepath.FromSlash(tc.want) // for win/unix testing interoperability
			require.Equal(t, want, got)
		})
	}
}

func TestMungeLocation(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	cwd = filepath.ToSlash(cwd)

	root, err := filepath.Abs("/")
	require.NoError(t, err)
	t.Log(root)
	root = filepath.ToSlash(root)

	cwdWant := "sqlite3://" + cwd + "/sakila.db"

	t.Log("cwdWant:", cwdWant)
	t.Log("root:", root)
	testCases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{
			in:      "",
			wantErr: true,
		},
		{
			in:   "sqlite3:///path/to/sakila.db",
			want: "sqlite3://" + root + "path/to/sakila.db",
		},
		{
			in:   "sqlite3://sakila.db",
			want: cwdWant,
		},
		{
			in:   "sqlite3:sakila.db",
			want: cwdWant,
		},
		{
			in:   "sqlite3:/sakila.db",
			want: "sqlite3://" + root + "sakila.db",
		},
		{
			in:   "sakila.db",
			want: cwdWant,
		},
		{
			in:   "./sakila.db",
			want: cwdWant,
		},
		{
			in:   "/path/to/sakila.db",
			want: "sqlite3://" + root + "path/to/sakila.db",
		},
	}

	for i, tc := range testCases {
		tc := tc
		t.Run(tutil.Name(i, tc.in), func(t *testing.T) {
			got, err := sqlite3.MungeLocation(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestSQLQuery_Whitespace(t *testing.T) {
	t.Parallel()

	th := testh.New(t)
	src := th.Source(sakila.SL3Whitespace)

	sink, err := th.QuerySQL(src, `SELECT * FROM "film actor"`)
	require.NoError(t, err)
	require.Equal(t, sakila.TblFilmActorCount, len(sink.Recs))

	sink, err = th.QuerySQL(src, `SELECT * FROM "actor"`)
	require.NoError(t, err)
	require.Equal(t, "first name", sink.RecMeta[1].Name())
	require.Equal(t, "first name", sink.RecMeta[1].MungedName())
	require.Equal(t, "last name", sink.RecMeta[2].Name())
	require.Equal(t, "last name", sink.RecMeta[2].MungedName())
}

func TestExtension_fts5(t *testing.T) {
	const tblActorFts = "actor_fts"

	th := testh.New(t)
	src := th.Add(&source.Source{
		Handle:   "@fts",
		Type:     sqlite3.Type,
		Location: "sqlite3://" + tutil.MustAbsFilepath("testdata", "sakila_fts5.db"),
	})

	srcMeta, err := th.SourceMetadata(src)
	require.NoError(t, err)
	require.Equal(t, src.Handle, srcMeta.Handle)
	tblMeta1 := srcMeta.Table(tblActorFts)
	require.NotNil(t, tblMeta1)
	require.Equal(t, tblActorFts, tblMeta1.Name)
	require.Equal(t, sqlz.TableTypeVirtual, tblMeta1.TableType)

	require.Equal(t, "actor_id", tblMeta1.Columns[0].Name)
	require.Equal(t, "integer", tblMeta1.Columns[0].ColumnType)
	require.Equal(t, "integer", tblMeta1.Columns[0].BaseType)
	require.Equal(t, kind.Int, tblMeta1.Columns[0].Kind)
	require.Equal(t, "first_name", tblMeta1.Columns[1].Name)
	require.Equal(t, "text", tblMeta1.Columns[1].ColumnType)
	require.Equal(t, "text", tblMeta1.Columns[1].BaseType)
	require.Equal(t, kind.Text, tblMeta1.Columns[1].Kind)
	require.Equal(t, "last_name", tblMeta1.Columns[2].Name)
	require.Equal(t, "text", tblMeta1.Columns[2].ColumnType)
	require.Equal(t, "text", tblMeta1.Columns[2].BaseType)
	require.Equal(t, kind.Text, tblMeta1.Columns[2].Kind)
	require.Equal(t, "last_update", tblMeta1.Columns[3].Name)
	require.Equal(t, "text", tblMeta1.Columns[3].ColumnType)
	require.Equal(t, "text", tblMeta1.Columns[3].BaseType)
	require.Equal(t, kind.Text, tblMeta1.Columns[3].Kind)

	tblMeta2, err := th.TableMetadata(src, tblActorFts)
	require.NoError(t, err)
	require.Equal(t, tblActorFts, tblMeta2.Name)
	require.Equal(t, sqlz.TableTypeVirtual, tblMeta2.TableType)
	require.EqualValues(t, *tblMeta1, *tblMeta2)

	// Verify that the (non-virtual) "actor" table has its type set correctly.
	actorMeta1 := srcMeta.Table(sakila.TblActor)
	actorMeta2, err := th.TableMetadata(src, sakila.TblActor)
	require.NoError(t, err)
	require.Equal(t, actorMeta1.TableType, sqlz.TableTypeTable)
	require.Equal(t, *actorMeta1, *actorMeta2)
}
