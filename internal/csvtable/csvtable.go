// Package csvtable reads and writes the CSV tables of data/.
//
// A field is quoted only when it holds a comma, a quote, or a newline, so a
// text column may hold any byte and the table stays readable. The reader
// accepts a quoted and an unquoted field, so a table written by hand parses.
package csvtable

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Table is one CSV file held in memory: a header line and the rows below it.
type Table struct {
	HeaderColumnNames []string
	DataRows          []map[string]string
}

// NewTable returns an empty table with the given column names.
func NewTable(headerColumnNames []string) *Table {
	return &Table{HeaderColumnNames: headerColumnNames}
}

// AppendRow adds one row. A column the row leaves out is written empty.
func (table *Table) AppendRow(row map[string]string) {
	filled := make(map[string]string, len(table.HeaderColumnNames))
	for _, columnName := range table.HeaderColumnNames {
		filled[columnName] = row[columnName]
	}
	table.DataRows = append(table.DataRows, filled)
}

// RowCount returns the number of data rows, not counting the header line.
func (table *Table) RowCount() int {
	return len(table.DataRows)
}

// ReadTable reads one CSV file into a Table. A missing file is an error, so a
// stage that needs an earlier stage says which table is absent.
func ReadTable(filePath string) (*Table, error) {
	fileHandle, openError := os.Open(filePath)
	if openError != nil {
		return nil, fmt.Errorf("cannot open table %s: %w", filePath, openError)
	}
	defer fileHandle.Close()

	csvReader := csv.NewReader(fileHandle)
	csvReader.FieldsPerRecord = -1
	csvReader.LazyQuotes = true
	allRecords, readError := csvReader.ReadAll()
	if readError != nil {
		return nil, fmt.Errorf("cannot parse table %s: %w", filePath, readError)
	}
	if len(allRecords) == 0 {
		return nil, fmt.Errorf("table %s holds no header line", filePath)
	}
	table := &Table{HeaderColumnNames: allRecords[0]}
	for _, record := range allRecords[1:] {
		if len(record) == 0 {
			continue
		}
		row := make(map[string]string, len(table.HeaderColumnNames))
		for columnIndex, columnName := range table.HeaderColumnNames {
			if columnIndex < len(record) {
				row[columnName] = record[columnIndex]
			}
		}
		table.DataRows = append(table.DataRows, row)
	}
	return table, nil
}

// ReadTableIfPresent reads a table and returns an empty table when the file
// does not exist, for a stage that may run before the table is written.
func ReadTableIfPresent(filePath string, headerColumnNames []string) (*Table, error) {
	if _, statError := os.Stat(filePath); statError != nil {
		return NewTable(headerColumnNames), nil
	}
	return ReadTable(filePath)
}

// WriteTable writes the table, quoting a field only when it needs it.
func (table *Table) WriteTable(filePath string) error {
	createError := os.MkdirAll(filepath.Dir(filePath), 0o755)
	if createError != nil {
		return fmt.Errorf("cannot create folder for %s: %w", filePath, createError)
	}
	temporaryPath := filePath + ".tmp"
	fileHandle, openError := os.Create(temporaryPath)
	if openError != nil {
		return fmt.Errorf("cannot create %s: %w", temporaryPath, openError)
	}
	csvWriter := csv.NewWriter(fileHandle)
	writeError := csvWriter.Write(table.HeaderColumnNames)
	if writeError == nil {
		for _, row := range table.DataRows {
			record := make([]string, len(table.HeaderColumnNames))
			for columnIndex, columnName := range table.HeaderColumnNames {
				record[columnIndex] = row[columnName]
			}
			writeError = csvWriter.Write(record)
			if writeError != nil {
				break
			}
		}
	}
	csvWriter.Flush()
	if writeError == nil {
		writeError = csvWriter.Error()
	}
	closeError := fileHandle.Close()
	if writeError != nil {
		return fmt.Errorf("cannot write %s: %w", filePath, writeError)
	}
	if closeError != nil {
		return fmt.Errorf("cannot close %s: %w", filePath, closeError)
	}
	return os.Rename(temporaryPath, filePath)
}

// AppendRowsToTable adds rows to an existing table and rewrites it, keeping
// the header line of the file on disk.
func AppendRowsToTable(filePath string, rows []map[string]string) (int, error) {
	existingTable, readError := ReadTable(filePath)
	if readError != nil {
		return 0, readError
	}
	knownValues := make(map[string]bool, len(existingTable.DataRows))
	for _, row := range existingTable.DataRows {
		knownValues[row["window_id"]] = true
	}
	addedCount := 0
	for _, row := range rows {
		if knownValues[row["window_id"]] {
			continue
		}
		existingTable.AppendRow(row)
		addedCount++
	}
	if addedCount == 0 {
		return 0, nil
	}
	return addedCount, existingTable.WriteTable(filePath)
}

// RelativePath returns the path of a file below the project folder, so a log
// line names a short path.
func RelativePath(projectFolderPath string, filePath string) string {
	relativePath, relativeError := filepath.Rel(projectFolderPath, filePath)
	if relativeError != nil {
		return filePath
	}
	if strings.HasPrefix(relativePath, "..") {
		return filePath
	}
	return relativePath
}
