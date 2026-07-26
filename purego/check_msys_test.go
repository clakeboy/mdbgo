package purego

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestCheckMSysQueriesCols(t *testing.T) {
	mdb, err := OpenMDB("../testdb/mdbs/ABIQuery.mdb")
	if err != nil {
		t.Fatal(err)
	}
	defer mdb.Close()

	entry := mdb.handle.GetCatalogEntryByName("MSysQueries")
	if entry == nil {
		t.Fatal("MSysQueries not found")
	}
	table := mdb.handle.ReadTable(entry)
	if table == nil {
		t.Fatal("Cannot read MSysQueries")
	}
	defer mdb.handle.FreeTableDef(table)
	mdb.handle.ReadColumns(table)

	for _, col := range table.Columns {
		typeName := ColumnTypeName(col.ColType)
		fmt.Printf("  %s: Type=%s(%d)\n", col.Name, typeName, col.ColType)
	}
}

func TestCheckMSysQueriesData(t *testing.T) {
	mdb, err := OpenMDB("../testdb/mdbs/ABIQuery.mdb")
	if err != nil {
		t.Fatal(err)
	}
	defer mdb.Close()

	entry := mdb.handle.GetCatalogEntryByName("MSysQueries")
	table := mdb.handle.ReadTable(entry)
	if table == nil {
		t.Fatal("Cannot read MSysQueries")
	}
	defer mdb.handle.FreeTableDef(table)
	mdb.handle.ReadColumns(table)

	attrBuf := make([]byte, 4000)
	exprBuf := make([]byte, 4000)
	flagBuf := make([]byte, 4000)
	name1Buf := make([]byte, 4000)
	name2Buf := make([]byte, 4000)
	oidBuf := make([]byte, 4000)

	mdb.handle.BindColumnByName(table, "Attribute", attrBuf, nil)
	mdb.handle.BindColumnByName(table, "Expression", exprBuf, nil)
	mdb.handle.BindColumnByName(table, "Flag", flagBuf, nil)
	mdb.handle.BindColumnByName(table, "Name1", name1Buf, nil)
	mdb.handle.BindColumnByName(table, "Name2", name2Buf, nil)
	mdb.handle.BindColumnByName(table, "ObjectId", oidBuf, nil)

	// Find ObjectId for v_sys_group_branch_user
	viewId := findViewObjectId(mdb, "v_sys_group_branch_user")
	if viewId == 0 {
		t.Fatal("view not found")
	}
	fmt.Printf("v_sys_group_branch_user ObjectId=%d\n", viewId)

	table.RewindTable()
	count := 0
	for table.FetchRow() {
		oidStr := strings.TrimRight(string(oidBuf), "\x00")
		oidVal, _ := strconv.Atoi(oidStr)
		oidVal &= 0x00FFFFFF
		if oidVal != viewId {
			continue
		}

		attrStr := strings.TrimRight(string(attrBuf), "\x00")
		flagStr := strings.TrimRight(string(flagBuf), "\x00")
		attr, _ := strconv.Atoi(attrStr)

		fmt.Printf("Row Attr=%d Flag=%s\n", attr, flagStr)
		fmt.Printf("  Name1 raw hex (%d): %x\n", len(name1Buf), name1Buf[:60])
		fmt.Printf("  Name1 UTF16: %q\n", UTF16LEToString(name1Buf))
		fmt.Printf("  Name2 raw hex (%d): %x\n", len(name2Buf), name2Buf[:60])
		fmt.Printf("  Name2 UTF16: %q\n", UTF16LEToString(name2Buf))
		fmt.Printf("  Expression raw hex (%d): %x\n", len(exprBuf), exprBuf[:120])
		fmt.Printf("  Expression UTF16: %q\n", UTF16LEToString(exprBuf))
		fmt.Printf("  Expression string: %q\n", strings.TrimRight(string(exprBuf), "\x00"))
		fmt.Println()
		count++
	}
	fmt.Printf("Total rows for view: %d\n", count)
}

func findViewObjectId(mdb *MDB, viewName string) int {
	mobj := mdb.handle.GetCatalogEntryByName("MSysObjects")
	if mobj == nil {
		return 0
	}
	table := mdb.handle.ReadTable(mobj)
	if table == nil {
		return 0
	}
	defer mdb.handle.FreeTableDef(table)
	mdb.handle.ReadColumns(table)

	idBuf := make([]byte, 4000)
	nameBuf := make([]byte, 4000)
	mdb.handle.BindColumnByName(table, "Id", idBuf, nil)
	mdb.handle.BindColumnByName(table, "Name", nameBuf, nil)

	table.RewindTable()
	for table.FetchRow() {
		nameStr := UTF16LEToString(nameBuf)
		if strings.EqualFold(nameStr, viewName) {
			idStr := strings.TrimRight(string(idBuf), "\x00")
			idVal, _ := strconv.Atoi(idStr)
			return idVal & 0x00FFFFFF
		}
	}
	return 0
}
