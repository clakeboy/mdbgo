package mdbgo

import (
	"context"
	"strings"
	"testing"
)

func TestNegativeLongIntegerFromMDB(t *testing.T) {
	db, err := Open("testdb/mdbs/ics2.mdb")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	table, err := db.ReadTable("t_oem_tb_status")
	if err != nil {
		t.Fatalf("ReadTable failed: %v", err)
	}
	statusIDColumn := -1
	for i, name := range table.Columns {
		if strings.EqualFold(name, "status_id") {
			statusIDColumn = i
			break
		}
	}
	if statusIDColumn < 0 {
		t.Fatalf("status_id column not found: %v", table.Columns)
	}

	foundNegative := false
	for _, row := range table.Rows {
		if statusIDColumn < len(row) && row[statusIDColumn] == "-19" {
			foundNegative = true
			break
		}
	}
	if !foundNegative {
		t.Fatalf("ReadTable did not preserve status_id=-19: %v", table.Rows)
	}

	rows, err := db.QueryContext(
		context.Background(),
		"SELECT [status_id] FROM [t_oem_tb_status] ORDER BY [status_id] ASC LIMIT 1",
		nil,
	)
	if err != nil {
		t.Fatalf("QueryContext failed: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("QueryContext returned no rows: %v", rows.Err())
	}
	values := rows.Values()
	if len(values) != 1 || values[0].Kind != ValueInt || values[0].Int != -19 {
		t.Fatalf("QueryContext status_id=%#v, want -19", values)
	}
}
