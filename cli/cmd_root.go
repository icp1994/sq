package cli

import (
	"github.com/neilotoole/sq/cli/flag"
	"github.com/spf13/cobra"

	// Import the providers package to initialize provider implementations.
	_ "github.com/neilotoole/sq/drivers"
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   `sq QUERY`,
		Short: "sq",
		Long: `sq is a swiss-army knife for wrangling data.

  $ sq '@sakila_pg | .actor | where(.actor_id > 2) | .first_name, .last_name | .[0:10]'

Use sq to query Postgres, SQLite, SQLServer, MySQL, CSV, Excel, etc,
and output in text, JSON, CSV, Excel and so on, or write output to a
database table.

You can query using sq's own jq-like syntax, or in native SQL.

Use "sq inspect" to view schema metadata. Use the "sq tbl" commands
to copy, truncate and drop tables. Use "sq diff" to compare source metadata
and row data.

See docs and more: https://sq.io`,
		Example: `# Add Postgres source identified by handle @sakila_pg
  $ sq add --handle=@sakila_pg 'postgres://user:pass@localhost:5432/sakila'

  # List available data sources.
  $ sq ls

  # Set active data source.
  $ sq src @sakila_pg

  # Get specified cols from table address in active data source.
  $ sq '.actor | .actor_id, .first_name, .last_name'

  # Ping a data source.
  $ sq ping @sakila_pg

  # View metadata (schema, stats etc) for data source.
  $ sq inspect @sakila_pg

  # View metadata for a table.
  $ sq inspect @sakila_pg.actor

  # Output all rows from 'actor' table in JSON.
  $ sq -j .actor

  # Alternative way to specify format.
  $ sq --format json .actor

  # Output in text format (with header).
  $ sq -th .actor

  # Output in text format (no header).
  $ sq -tH .actor

  # Output to a HTML file.
  $ sq --html '@sakila_pg.actor' -o actor.html

  # Join across data sources.
  $ sq '@my1.person, @pg1.address | join(.uid) | .username, .email, .city'

  # Insert query results into a table in another data source.
  $ sq --insert=@pg1.person '@my1.person | .username, .email'

  # Execute a database-native SQL query, specifying the source.
  $ sq sql --src=@pg1 'SELECT uid, username, email FROM person LIMIT 2'

  # Copy a table (in the same source).
  $ sq tbl copy @sakila_pg.actor .actor2

  # Truncate table.
  $ sq tbl truncate @sakila_pg.actor2

  # Drop table.
  $ sq tbl drop @sakila_pg.actor2

  # Pipe an Excel file and output the first 10 rows from sheet1
  $ cat data.xlsx | sq '.sheet1 | .[0:10]'`,
	}

	cmd.Flags().SortFlags = false
	cmd.PersistentFlags().SortFlags = false

	// The --help flag must be explicitly added to rootCmd,
	// or else cobra tries to do its own (unwanted) thing.
	// The behavior of cobra in this regard seems to have
	// changed? This particular incantation currently does the trick.
	cmd.PersistentFlags().Bool(flag.Help, false, "Show help")

	addQueryCmdFlags(cmd)
	cmd.Flags().Bool(flag.Version, false, flag.VersionUsage)

	cmd.PersistentFlags().BoolP(flag.Monochrome, flag.MonochromeShort, false, flag.MonochromeUsage)
	cmd.PersistentFlags().BoolP(flag.Verbose, flag.VerboseShort, false, flag.VerboseUsage)

	cmd.PersistentFlags().String(flag.Config, "", flag.ConfigUsage)

	cmd.PersistentFlags().Bool(flag.LogEnabled, false, flag.LogEnabledUsage)
	cmd.PersistentFlags().String(flag.LogFile, "", flag.LogFileUsage)
	cmd.PersistentFlags().String(flag.LogLevel, "", flag.LogLevelUsage)
	return cmd
}
