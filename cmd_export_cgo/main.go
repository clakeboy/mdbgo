package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/clakeboy/mdbgo"
)

func main() {
	db, err := mdbgo.Open("/home/clake/projects/go_projects/mdbgo/testdb/mdbs/ABIQuery.mdb")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Open: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	td, err := db.ReadTable("t_abi_hbl")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ReadTable: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Create("/home/clake/projects/go_projects/mdbgo/testdb/cgo_export")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Create: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	w.WriteString(strings.Join(td.Columns, "\t") + "\n")
	for _, row := range td.Rows {
		w.WriteString(strings.Join(row, "\t") + "\n")
	}
	w.Flush()
	fmt.Printf("done: %d cols, %d rows\n", len(td.Columns), len(td.Rows))
}
