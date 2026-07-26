package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/clakeboy/mdbgo/purego"
)

func main() {
	mdb, err := purego.OpenMDB("/home/clake/projects/go_projects/mdbgo/testdb/mdbs/ABIQuery.mdb")
	if err != nil {
		fmt.Fprintf(os.Stderr, "OpenMDB: %v\n", err)
		os.Exit(1)
	}
	defer mdb.Close()

	schema, err := mdb.GetTableSchema("t_abi_hbl")
	if err != nil {
		fmt.Fprintf(os.Stderr, "GetTableSchema: %v\n", err)
		os.Exit(1)
	}

	data, err := mdb.ReadTableData("t_abi_hbl")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ReadTableData: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Create("/home/clake/projects/go_projects/mdbgo/testdb/go_export")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Create: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	headers := make([]string, len(schema.Columns))
	for i, c := range schema.Columns {
		headers[i] = c.Name
	}
	w.WriteString(strings.Join(headers, "\t") + "\n")
	for _, row := range data {
		w.WriteString(strings.Join(row, "\t") + "\n")
	}
	w.Flush()
	fmt.Printf("done: %d cols, %d rows\n", len(headers), len(data))
}
