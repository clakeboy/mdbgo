package mdbgo

import (
	"context"
	"strings"
	"testing"
)

func TestCompositeJoinSavedQueryFromMDB(t *testing.T) {
	db, err := Open("testdb/mdbs/ics2.mdb")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	const viewName = "s_cvm"
	sqlText, err := db.ViewSQL(viewName)
	if err != nil {
		t.Fatalf("ViewSQL(%q) failed: %v", viewName, err)
	}
	for _, condition := range []string{
		"t_cvm.ics_company_id = t_ics_company_branch.ics_company_id",
		"t_cvm.ics_branch_id = t_ics_company_branch.ics_branch_id",
	} {
		if !strings.Contains(sqlText, condition) {
			t.Fatalf("ViewSQL(%q) missing composite condition %q:\n%s", viewName, condition, sqlText)
		}
	}

	rows, err := db.QueryContext(context.Background(), "SELECT TOP 1 * FROM ["+viewName+"]", nil)
	if err != nil {
		t.Fatalf("query saved view %q failed:\n%s\nerror: %v", viewName, sqlText, err)
	}
	defer rows.Close()
	if len(rows.Columns()) == 0 {
		t.Fatalf("query saved view %q returned no columns", viewName)
	}
	if !rows.Next() {
		t.Fatalf("query saved view %q returned no data rows: %v", viewName, rows.Err())
	}
}
