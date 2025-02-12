// Package sqlserver implements the sq driver for SQL Server.
package sqlserver

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/neilotoole/sq/libsq/core/loz"

	"github.com/neilotoole/sq/libsq/core/jointype"

	"github.com/neilotoole/sq/libsq/core/record"

	"github.com/neilotoole/sq/libsq/driver/dialect"

	"github.com/neilotoole/sq/libsq/core/lg/lga"

	"github.com/neilotoole/sq/libsq/core/lg/lgm"

	"github.com/neilotoole/sq/libsq/core/lg"

	"github.com/neilotoole/sq/libsq/ast/render"
	"github.com/neilotoole/sq/libsq/core/errz"
	"github.com/neilotoole/sq/libsq/core/kind"
	"github.com/neilotoole/sq/libsq/core/sqlmodel"
	"github.com/neilotoole/sq/libsq/core/sqlz"
	"github.com/neilotoole/sq/libsq/core/stringz"
	"github.com/neilotoole/sq/libsq/driver"
	"github.com/neilotoole/sq/libsq/source"
)

const (
	// Type is the SQL Server source driver type.
	Type = source.DriverType("sqlserver")

	// dbDrvr is the backing SQL Server driver impl name.
	dbDrvr = "sqlserver"
)

var _ driver.Provider = (*Provider)(nil)

// Provider is the SQL Server implementation of driver.Provider.
type Provider struct {
	Log *slog.Logger
}

// DriverFor implements driver.Provider.
func (p *Provider) DriverFor(typ source.DriverType) (driver.Driver, error) {
	if typ != Type {
		return nil, errz.Errorf("unsupported driver type {%s}}", typ)
	}

	return &driveri{log: p.Log}, nil
}

var _ driver.SQLDriver = (*driveri)(nil)

// driveri is the SQL Server implementation of driver.Driver.
type driveri struct {
	log *slog.Logger
}

// ConnParams implements driver.SQLDriver.
func (d *driveri) ConnParams() map[string][]string {
	// https://github.com/microsoft/go-mssqldb#connection-parameters-and-dsn.
	return map[string][]string{
		"ApplicationIntent":      {"ReadOnly"},
		"ServerSPN":              nil,
		"TrustServerCertificate": {"false", "true"},
		"Workstation ID":         nil,
		"app name":               {"sq"},
		"certificate":            nil,
		"connection timeout":     {"0"},
		"database":               nil,
		"dial timeout":           {"0"},
		"encrypt":                {"disable", "false", "true"},
		"failoverpartner":        nil,
		"failoverport":           {"1433"},
		"hostNameInCertificate":  nil,
		"keepAlive":              {"0", "30"},
		"log":                    {"0", "1", "2", "4", "8", "16", "32", "64", "128", "255"},
		"packet size":            {"512", "4096", "16383", "32767"},
		"protocol":               nil,
		"tlsmin":                 {"1.0", "1.1", "1.2", "1.3"},
		"user id":                nil,
	}
}

// ErrWrapFunc implements driver.SQLDriver.
func (d *driveri) ErrWrapFunc() func(error) error {
	return errw
}

// DriverMetadata implements driver.SQLDriver.
func (d *driveri) DriverMetadata() driver.Metadata {
	return driver.Metadata{
		Type:        Type,
		Description: "Microsoft SQL Server / Azure SQL Edge",
		Doc:         "https://github.com/microsoft/go-mssqldb",
		IsSQL:       true,
		DefaultPort: 1433,
	}
}

// Dialect implements driver.SQLDriver.
func (d *driveri) Dialect() dialect.Dialect {
	return dialect.Dialect{
		Type:           Type,
		Placeholders:   placeholders,
		Enquote:        stringz.DoubleQuote,
		MaxBatchValues: 1000,
		Ops:            dialect.DefaultOps(),
		Joins:          jointype.All(),
	}
}

func placeholders(numCols, numRows int) string {
	rows := make([]string, numRows)

	n := 1
	var sb strings.Builder
	for i := 0; i < numRows; i++ {
		sb.Reset()
		sb.WriteRune('(')
		for j := 1; j <= numCols; j++ {
			sb.WriteString("@p")
			sb.WriteString(strconv.Itoa(n))
			n++
			if j < numCols {
				sb.WriteString(driver.Comma)
			}
		}
		sb.WriteRune(')')
		rows[i] = sb.String()
	}

	return strings.Join(rows, driver.Comma)
}

// Renderer implements driver.SQLDriver.
func (d *driveri) Renderer() *render.Renderer {
	r := render.NewDefaultRenderer()

	// Custom functions for SQLServer-specific stuff.
	r.Range = renderRange
	r.PreRender = preRender

	return r
}

// Open implements driver.DatabaseOpener.
func (d *driveri) Open(ctx context.Context, src *source.Source) (driver.Database, error) {
	lg.FromContext(ctx).Debug(lgm.OpenSrc, lga.Src, src)

	db, err := d.doOpen(ctx, src)
	if err != nil {
		return nil, err
	}

	if err = driver.OpeningPing(ctx, src, db); err != nil {
		return nil, err
	}

	return &database{log: d.log, db: db, src: src, drvr: d}, nil
}

func (d *driveri) doOpen(ctx context.Context, src *source.Source) (*sql.DB, error) {
	db, err := sql.Open(dbDrvr, src.Location)
	if err != nil {
		return nil, errw(err)
	}

	driver.ConfigureDB(ctx, db, src.Options)
	return db, nil
}

// ValidateSource implements driver.Driver.
func (d *driveri) ValidateSource(src *source.Source) (*source.Source, error) {
	if src.Type != Type {
		return nil, errz.Errorf("expected driver type %q but got %q", Type, src.Type)
	}
	return src, nil
}

// Ping implements driver.Driver.
func (d *driveri) Ping(ctx context.Context, src *source.Source) error {
	db, err := d.doOpen(ctx, src)
	if err != nil {
		return err
	}
	defer lg.WarnIfCloseError(d.log, lgm.CloseDB, db)

	err = db.PingContext(ctx)
	return errz.Wrapf(errw(err), "ping %s", src.Handle)
}

// DBProperties implements driver.SQLDriver.
func (d *driveri) DBProperties(ctx context.Context, db sqlz.DB) (map[string]any, error) {
	return getDBProperties(ctx, db)
}

// Truncate implements driver.Driver. Due to a quirk of SQL Server, the
// operation is implemented in two statements. First "DELETE FROM tbl" to
// delete all rows. Then, if reset is true, the table sequence counter
// is reset via RESEED.
//
//nolint:lll
func (d *driveri) Truncate(ctx context.Context, src *source.Source, tbl string, reset bool,
) (affected int64, err error) {
	// https://docs.microsoft.com/en-us/sql/t-sql/statements/truncate-table-transact-sql?view=sql-server-ver15

	// When there are foreign key constraints on mssql tables,
	// it's not possible to TRUNCATE the table. An alternative is
	// to delete all rows and reseed the identity column.
	//
	//  DELETE FROM "table1"; DBCC CHECKIDENT ('table1', RESEED, 1);
	//
	// See: https://stackoverflow.com/questions/253849/cannot-truncate-table-because-it-is-being-referenced-by-a-foreign-key-constraint

	db, err := d.doOpen(ctx, src)
	if err != nil {
		return 0, errw(err)
	}
	defer lg.WarnIfFuncError(d.log, lgm.CloseDB, db.Close)

	affected, err = sqlz.ExecAffected(ctx, db, fmt.Sprintf("DELETE FROM %q", tbl))
	if err != nil {
		return affected, errz.Wrapf(errw(err), "truncate: failed to delete from %q", tbl)
	}

	if reset {
		_, err = db.ExecContext(ctx, fmt.Sprintf("DBCC CHECKIDENT ('%s', RESEED, 1)", tbl))
		if err != nil {
			return affected, errz.Wrapf(errw(err), "truncate: deleted %d rows from %q but RESEED failed", affected, tbl)
		}
	}

	return affected, nil
}

// TableColumnTypes implements driver.SQLDriver.
func (d *driveri) TableColumnTypes(ctx context.Context, db sqlz.DB, tblName string,
	colNames []string,
) ([]*sql.ColumnType, error) {
	// SQLServer has this unusual incantation for its LIMIT equivalent:
	//
	// SELECT username, email, address_id FROM person
	// ORDER BY (SELECT 0) OFFSET 0 ROWS FETCH NEXT 1 ROWS ONLY;
	const queryTpl = "SELECT %s FROM %s ORDER BY (SELECT 0) OFFSET 0 ROWS FETCH NEXT 1 ROWS ONLY"

	enquote := d.Dialect().Enquote
	tblNameQuoted := enquote(tblName)

	colsClause := "*"
	if len(colNames) > 0 {
		colNamesQuoted := loz.Apply(colNames, enquote)
		colsClause = strings.Join(colNamesQuoted, driver.Comma)
	}

	query := fmt.Sprintf(queryTpl, colsClause, tblNameQuoted)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, errw(err)
	}

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		lg.WarnIfFuncError(d.log, lgm.CloseDBRows, rows.Close)
		return nil, errw(err)
	}

	err = rows.Err()
	if err != nil {
		lg.WarnIfFuncError(d.log, lgm.CloseDBRows, rows.Close)
		return nil, errw(err)
	}

	err = rows.Close()
	if err != nil {
		return nil, errw(err)
	}

	return colTypes, nil
}

// RecordMeta implements driver.SQLDriver.
func (d *driveri) RecordMeta(ctx context.Context, colTypes []*sql.ColumnType) (
	record.Meta, driver.NewRecordFunc, error,
) {
	sColTypeData := make([]*record.ColumnTypeData, len(colTypes))
	ogColNames := make([]string, len(colTypes))
	for i, colType := range colTypes {
		kind := kindFromDBTypeName(d.log, colType.Name(), colType.DatabaseTypeName())
		colTypeData := record.NewColumnTypeData(colType, kind)
		setScanType(colTypeData, kind)
		sColTypeData[i] = colTypeData
		ogColNames[i] = colTypeData.Name
	}

	mungedColNames, err := driver.MungeResultColNames(ctx, ogColNames)
	if err != nil {
		return nil, nil, err
	}

	recMeta := make(record.Meta, len(colTypes))
	for i := range sColTypeData {
		recMeta[i] = record.NewFieldMeta(sColTypeData[i], mungedColNames[i])
	}

	mungeFn := func(vals []any) (record.Record, error) {
		// sqlserver doesn't need to do any special munging, so we
		// just use the default munging.
		rec, skipped := driver.NewRecordFromScanRow(recMeta, vals, nil)
		if len(skipped) > 0 {
			return nil, errz.Errorf("expected zero skipped cols but have %d", skipped)
		}
		return rec, nil
	}

	return recMeta, mungeFn, nil
}

// TableExists implements driver.SQLDriver.
func (d *driveri) TableExists(ctx context.Context, db sqlz.DB, tbl string) (bool, error) {
	const query = `SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema = 'dbo' AND table_name = @p1`

	var count int64
	err := db.QueryRowContext(ctx, query, tbl).Scan(&count)
	if err != nil {
		return false, errw(err)
	}

	return count == 1, nil
}

// CurrentSchema implements driver.SQLDriver.
func (d *driveri) CurrentSchema(ctx context.Context, db sqlz.DB) (string, error) {
	var name string
	if err := db.QueryRowContext(ctx, `SELECT SCHEMA_NAME()`).Scan(&name); err != nil {
		return "", errw(err)
	}

	return name, nil
}

// CreateTable implements driver.SQLDriver.
func (d *driveri) CreateTable(ctx context.Context, db sqlz.DB, tblDef *sqlmodel.TableDef) error {
	stmt := buildCreateTableStmt(tblDef)

	_, err := db.ExecContext(ctx, stmt)
	return errw(err)
}

// AlterTableAddColumn implements driver.SQLDriver.
func (d *driveri) AlterTableAddColumn(ctx context.Context, db sqlz.DB, tbl, col string, knd kind.Kind) error {
	q := fmt.Sprintf("ALTER TABLE %q ADD %q ", tbl, col) + dbTypeNameFromKind(knd)

	_, err := db.ExecContext(ctx, q)
	return errz.Wrapf(errw(err), "alter table: failed to add column %q to table %q", col, tbl)
}

// AlterTableRename implements driver.SQLDriver.
func (d *driveri) AlterTableRename(ctx context.Context, db sqlz.DB, tbl, newName string) error {
	schema, err := d.CurrentSchema(ctx, db)
	if err != nil {
		return err
	}

	q := fmt.Sprintf(`exec sp_rename '[%s].[%s]', '%s'`, schema, tbl, newName)
	_, err = db.ExecContext(ctx, q)
	return errz.Wrapf(errw(err), "alter table: failed to rename table %q to %q", tbl, newName)
}

// AlterTableRenameColumn implements driver.SQLDriver.
func (d *driveri) AlterTableRenameColumn(ctx context.Context, db sqlz.DB, tbl, col, newName string) error {
	schema, err := d.CurrentSchema(ctx, db)
	if err != nil {
		return err
	}

	q := fmt.Sprintf(`exec sp_rename '[%s].[%s].[%s]', '%s'`, schema, tbl, col, newName)
	_, err = db.ExecContext(ctx, q)
	return errz.Wrapf(errw(err), "alter table: failed to rename column {%s.%s.%s} to {%s}", schema, tbl, col, newName)
}

// CopyTable implements driver.SQLDriver.
func (d *driveri) CopyTable(ctx context.Context, db sqlz.DB, fromTable, toTable string, copyData bool) (int64, error) {
	var stmt string

	if copyData {
		stmt = fmt.Sprintf("SELECT * INTO %q FROM %q", toTable, fromTable)
	} else {
		stmt = fmt.Sprintf("SELECT TOP(0) * INTO %q FROM %q", toTable, fromTable)
	}

	affected, err := sqlz.ExecAffected(ctx, db, stmt)
	if err != nil {
		return 0, errw(err)
	}

	return affected, nil
}

// DropTable implements driver.SQLDriver.
func (d *driveri) DropTable(ctx context.Context, db sqlz.DB, tbl string, ifExists bool) error {
	var stmt string

	if ifExists {
		stmt = fmt.Sprintf("IF OBJECT_ID('dbo.%s', 'U') IS NOT NULL DROP TABLE dbo.%q", tbl, tbl)
	} else {
		stmt = fmt.Sprintf("DROP TABLE dbo.%q", tbl)
	}

	_, err := db.ExecContext(ctx, stmt)
	return errw(err)
}

// PrepareInsertStmt implements driver.SQLDriver.
func (d *driveri) PrepareInsertStmt(ctx context.Context, db sqlz.DB, destTbl string, destColNames []string,
	numRows int,
) (*driver.StmtExecer, error) {
	destColsMeta, err := d.getTableColsMeta(ctx, db, destTbl, destColNames)
	if err != nil {
		return nil, err
	}

	stmt, err := driver.PrepareInsertStmt(ctx, d, db, destTbl, destColsMeta.Names(), numRows)
	if err != nil {
		return nil, err
	}

	execer := driver.NewStmtExecer(stmt, driver.DefaultInsertMungeFunc(destTbl, destColsMeta),
		newStmtExecFunc(stmt, db, destTbl), destColsMeta)
	return execer, nil
}

// PrepareUpdateStmt implements driver.SQLDriver.
func (d *driveri) PrepareUpdateStmt(ctx context.Context, db sqlz.DB, destTbl string, destColNames []string,
	where string,
) (*driver.StmtExecer, error) {
	destColsMeta, err := d.getTableColsMeta(ctx, db, destTbl, destColNames)
	if err != nil {
		return nil, err
	}

	query, err := buildUpdateStmt(destTbl, destColNames, where)
	if err != nil {
		return nil, err
	}

	stmt, err := db.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}

	execer := driver.NewStmtExecer(stmt, driver.DefaultInsertMungeFunc(destTbl, destColsMeta),
		newStmtExecFunc(stmt, db, destTbl), destColsMeta)
	return execer, nil
}

func (d *driveri) getTableColsMeta(ctx context.Context, db sqlz.DB, tblName string, colNames []string) (
	record.Meta, error,
) {
	// SQLServer has this unusual incantation for its LIMIT equivalent:
	//
	// SELECT username, email, address_id FROM person
	// ORDER BY (SELECT 0) OFFSET 0 ROWS FETCH NEXT 1 ROWS ONLY;
	const queryTpl = "SELECT %s FROM %s ORDER BY (SELECT 0) OFFSET 0 ROWS FETCH NEXT 1 ROWS ONLY"

	enquote := d.Dialect().Enquote
	tblNameQuoted := enquote(tblName)
	colNamesQuoted := loz.Apply(colNames, enquote)
	colsJoined := strings.Join(colNamesQuoted, driver.Comma)

	query := fmt.Sprintf(queryTpl, colsJoined, tblNameQuoted)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, errw(err)
	}

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		lg.WarnIfFuncError(d.log, lgm.CloseDBRows, rows.Close)
		return nil, errw(err)
	}

	if rows.Err() != nil {
		return nil, errw(rows.Err())
	}

	destCols, _, err := d.RecordMeta(ctx, colTypes)
	if err != nil {
		lg.WarnIfFuncError(d.log, lgm.CloseDBRows, rows.Close)
		return nil, errw(err)
	}

	err = rows.Close()
	if err != nil {
		return nil, errw(err)
	}

	return destCols, nil
}

// database implements driver.Database.
type database struct {
	log  *slog.Logger
	drvr *driveri
	db   *sql.DB
	src  *source.Source
}

var _ driver.Database = (*database)(nil)

// DB implements driver.Database.
func (d *database) DB(context.Context) (*sql.DB, error) {
	return d.db, nil
}

// SQLDriver implements driver.Database.
func (d *database) SQLDriver() driver.SQLDriver {
	return d.drvr
}

// Source implements driver.Database.
func (d *database) Source() *source.Source {
	return d.src
}

// TableMetadata implements driver.Database.
func (d *database) TableMetadata(ctx context.Context, tblName string) (*source.TableMetadata, error) {
	const query = `SELECT TABLE_CATALOG, TABLE_SCHEMA, TABLE_TYPE
FROM INFORMATION_SCHEMA.TABLES
WHERE TABLE_NAME = @p1`

	var catalog, schema, tblType string
	err := d.db.QueryRowContext(ctx, query, tblName).Scan(&catalog, &schema, &tblType)
	if err != nil {
		return nil, errw(err)
	}

	// TODO: getTableMetadata can cause deadlock in the DB. Needs further investigation.
	// But a quick hack would be to use retry on a deadlock error.
	return getTableMetadata(ctx, d.db, catalog, schema, tblName, tblType)
}

// SourceMetadata implements driver.Database.
func (d *database) SourceMetadata(ctx context.Context, noSchema bool) (*source.Metadata, error) {
	return getSourceMetadata(ctx, d.src, d.db, noSchema)
}

// Close implements driver.Database.
func (d *database) Close() error {
	d.log.Debug(lgm.CloseDB, lga.Handle, d.src.Handle)

	return errw(d.db.Close())
}

// newStmtExecFunc returns a StmtExecFunc that has logic to deal with
// the "identity insert" error. If the error is encountered, setIdentityInsert
// is called and stmt is executed again.
func newStmtExecFunc(stmt *sql.Stmt, db sqlz.DB, tbl string) driver.StmtExecFunc {
	return func(ctx context.Context, args ...any) (int64, error) {
		res, err := stmt.ExecContext(ctx, args...)
		if err == nil {
			var affected int64
			affected, err = res.RowsAffected()
			return affected, errw(err)
		}

		if !hasErrCode(err, errCodeIdentityInsert) {
			return 0, errw(err)
		}

		idErr := setIdentityInsert(ctx, db, tbl, true)
		if idErr != nil {
			return 0, errz.Combine(errw(err), idErr)
		}

		res, err = stmt.ExecContext(ctx, args...)
		if err != nil {
			return 0, errw(err)
		}

		affected, err := res.RowsAffected()
		return affected, errw(err)
	}
}

// setIdentityInsert enables (or disables) "identity insert" for tbl on db.
// SQLServer is fussy about inserting values to the identity col. This
// error can be returned from the driver:
//
//	mssql: Cannot insert explicit value for identity column in table 'payment' when IDENTITY_INSERT is set to OFF
//
// The solution is "SET IDENTITY_INSERT tbl ON".
//
// See: https://docs.microsoft.com/en-us/sql/t-sql/statements/set-identity-insert-transact-sql?view=sql-server-ver15
func setIdentityInsert(ctx context.Context, db sqlz.DB, tbl string, on bool) error {
	mode := "ON"
	if !on {
		mode = "OFF"
	}

	query := fmt.Sprintf("SET IDENTITY_INSERT %q %s", tbl, mode)
	_, err := db.ExecContext(ctx, query)
	return errz.Wrapf(errw(err), "failed to SET IDENTITY INSERT %s %s", tbl, mode)
}
