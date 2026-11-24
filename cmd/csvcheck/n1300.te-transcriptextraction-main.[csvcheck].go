package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)
// checkTable checks one CSV file and returns the count of bad rows.
func checkTable(tableFilePath string) (int, int, int) {
	tableFile, openError := os.Open(tableFilePath)
	if openError != nil {
		fmt.Fprintf(os.Stderr, "FAIL: cannot open %s: %v\n", tableFilePath, openError)
		return 0, 0, 1
	}
	defer tableFile.Close()
	reader := csv.NewReader(tableFile)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	headerRecord, readError := reader.Read()
	if readError != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %s is empty or unreadable: %v\n", tableFilePath, readError)
		return 0, 0, 1
	}
	columnCount := len(headerRecord)
	rowCount := 0
	badCount := 0
	for {
		dataRecord, recordError := reader.Read()
		if recordError == io.EOF {
			break
		}
		if recordError != nil {
			fmt.Fprintf(os.Stderr, "FAIL: %s line %d: %v\n", tableFilePath, rowCount+2, recordError)
			badCount++
			break
		}
		rowCount++
		if len(dataRecord) != columnCount {
			fmt.Fprintf(os.Stderr, "FAIL: %s line %d has %d fields, header has %d\n",
				tableFilePath, rowCount+1, len(dataRecord), columnCount)
			badCount++
			break
		}
	}
	return rowCount, columnCount, badCount
}

// main checks every CSV file in the folder given as the first argument.
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: csvcheck <table-folder>")
		os.Exit(2)
	}
	dataFolderPath := os.Args[1]
	entryList, readError := os.ReadDir(dataFolderPath)
	if readError != nil {
		fmt.Fprintf(os.Stderr, "FAIL: cannot read %s: %v\n", dataFolderPath, readError)
		os.Exit(1)
	}
	tableNameList := make([]string, 0, len(entryList))
	for _, entry := range entryList {
		if !strings.HasSuffix(entry.Name(), ".csv") {
			continue
		}
		tableNameList = append(tableNameList, entry.Name())
	}
	sort.Strings(tableNameList)
	checkedCount := 0
	badTableCount := 0
	for _, tableName := range tableNameList {
		rowCount, columnCount, badCount := checkTable(filepath.Join(dataFolderPath, tableName))
		checkedCount++
		if badCount > 0 {
			badTableCount++
			continue
		}
		fmt.Printf("ok: %-28s %6d rows, %2d columns\n", tableName, rowCount, columnCount)
	}
	fmt.Printf("%d csv files checked, %d bad\n", checkedCount, badTableCount)
	if badTableCount > 0 {
		os.Exit(1)
	}
}
