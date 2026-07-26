package purego

import (
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

const testMDBPathDebug = "../testdb/mdbs/ABIQuery.mdb"

func TestDebugRowLayout(t *testing.T) {
	if _, err := os.Stat(testMDBPathDebug); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPathDebug)
	}

	handle, err := Open(testMDBPathDebug, MDBNoFlags)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer handle.Close()

	t.Logf("Jet version: 0x%x", handle.F.JetVersion)

	entry := &MdbCatalogEntry{
		Mdb:        handle,
		ObjectType: MDBTable,
		TablePg:    2,
		ObjectName: "MSysObjects",
	}

	table := handle.ReadTable(entry)
	if table == nil {
		t.Fatal("ReadTable failed for MSysObjects")
	}
	defer handle.FreeTableDef(table)

	cols := handle.ReadColumns(table)
	if cols == nil {
		t.Fatal("ReadColumns returned nil")
	}

	t.Logf("Table: NumCols=%d NumVarCols=%d NumRealIdxs=%d", table.NumCols, table.NumVarCols, table.NumRealIdxs)
	t.Logf("TabColsStartOffset=%d TabRidxEntrySize=%d", handle.Fmt.TabColsStartOffset, handle.Fmt.TabRidxEntrySize)
	t.Logf("CurPos for col defs: %d", int(handle.Fmt.TabColsStartOffset)+int(table.NumRealIdxs)*int(handle.Fmt.TabRidxEntrySize))

	// Dump raw column definition bytes directly from page buffer
	// First, read the table definition page to get column def bytes
	tablePgBuf := make([]byte, handle.Fmt.PgSize)
	copy(tablePgBuf, handle.PgBuf[:])
	curPos := int(handle.Fmt.TabColsStartOffset) + int(table.NumRealIdxs)*int(handle.Fmt.TabRidxEntrySize)
	colBuf := make([]byte, handle.Fmt.TabColEntrySize)
	for i := 0; i < int(table.NumCols); i++ {
		savedCurPos := curPos
		handle.ReadPgIfN(colBuf, &curPos, int(handle.Fmt.TabColEntrySize))
		t.Logf("  ColDef[%d] curPos=%d→%d raw: %x", i, savedCurPos, curPos, colBuf)
		// Show the key fields with explicit byte offsets
		t.Logf("    byte[0]=ColType=%d byte[5]=ColNum=%d byte[7..8]=VarColNum=%d",
			colBuf[0], colBuf[5], GetInt16(colBuf, 7))
		t.Logf("    byte[15]=Flags=0x%02x isFixed=%v byte[21..22]=FixedOffset=%d byte[23..24]=ColSize=%d",
			colBuf[15], colBuf[15]&0x01 != 0,
			int16(GetInt16(colBuf, 21)), GetInt16(colBuf, 23))
	}

	t.Logf("Columns (sorted by ColNum):")
	for i, col := range cols {
		t.Logf("  [%d] ColNum=%d Name=%q Type=%d(%s) Size=%d IsFixed=%v FixedOffset=%d VarColNum=%d",
			i, col.ColNum, col.Name, col.ColType, ColumnTypeName(col.ColType),
			col.ColSize, col.IsFixed, col.FixedOffset, col.VarColNum)
	}

	// Manually read first few rows to dump raw layout
	entry2 := &MdbCatalogEntry{
		Mdb:        handle,
		ObjectType: MDBTable,
		TablePg:    2,
		ObjectName: "MSysObjects",
	}
	table2 := handle.ReadTable(entry2)
	if table2 == nil {
		t.Fatal("ReadTable failed")
	}
	defer handle.FreeTableDef(table2)
	handle.ReadColumns(table2)

	// Bind all columns
	binds := make([][]byte, table2.NumCols)
	lens := make([]int, table2.NumCols)
	for i := range binds {
		binds[i] = make([]byte, handle.BindSize)
		table2.BindColumn(i+1, binds[i], &lens[i])
	}

	table2.RewindTable()
	rowCount := 0
	for table2.FetchRow() {
		rowCount++
		if rowCount > 6 {
			break
		}

		// FindRow to get raw data
		rowStart, rowSize, err := handle.FindRow(int(table2.CurRow - 1))
		if err != nil {
			continue
		}
		rowEnd := rowStart + rowSize - 1
		pgBuf := handle.PgBuf[:]

		// Parse header
		colCount := GetInt16(pgBuf, rowStart)
		colCountSize := 2
		bitmaskSz := (colCount + 7) / 8

		t.Logf("\n=== Row %d (RowNum=%d) raw layout ===", rowCount, table2.CurRow-1)
		t.Logf("  rowStart=%d rowSize=%d rowEnd=%d", rowStart, rowSize, rowEnd)
		t.Logf("  colCount=%d colCountSize=%d bitmaskSz=%d", colCount, colCountSize, bitmaskSz)

		// Null mask is at end of row
		nullMaskStart := rowEnd - bitmaskSz + 1
		nullMask := pgBuf[nullMaskStart : rowEnd+1]
		t.Logf("  nullMask bytes [%d..%d]: %v", nullMaskStart, rowEnd, nullMask)

		// Null mask bits: 1 = NOT null, 0 = null
		for i := 0; i < int(table2.NumCols); i++ {
			col := cols[i]
			byteNum := col.ColNum / 8
			bitNum := col.ColNum % 8
			isNull := true
			if byteNum < len(nullMask) {
				isNull = (1<<bitNum)&nullMask[byteNum] == 0
			}
			t.Logf("    col[%d] ColNum=%d %q isNull=%v", i, col.ColNum, col.Name, isNull)
		}

		// Read rowVarCols
		rowVarCols := 0
		if table2.NumVarCols > 0 {
			rowVarCols = GetInt16(pgBuf, rowEnd-bitmaskSz-1)
		}
		t.Logf("  rowVarCols=%d (from [%d])", rowVarCols, rowEnd-bitmaskSz-1)

		// Read variable column offsets
		var varColOffsets []int
		if table2.NumVarCols > 0 {
			varColOffsets = make([]int, rowVarCols+1)
			for i := 0; i < rowVarCols+1; i++ {
				varColOffsets[i] = GetInt16(pgBuf, rowEnd-bitmaskSz-3-i*2)
			}
			t.Logf("  varColOffsets (raw from page): %v", varColOffsets)
			// These are relative to rowStart
			for i := 0; i < rowVarCols+1; i++ {
				t.Logf("    varColOffset[%d] = %d (abs = %d)", i, varColOffsets[i], rowStart+varColOffsets[i])
			}
		}

		// Dump fixed columns area
		fixedAreaEnd := rowEnd - bitmaskSz - 3 - rowVarCols*2 - 1
		if table2.NumVarCols == 0 {
			fixedAreaEnd = rowEnd - bitmaskSz
		}
		fixedAreaSize := fixedAreaEnd - rowStart - colCountSize + 1
		t.Logf("  fixed area: [%d..%d] (size=%d bytes after colCount)",
			rowStart+colCountSize, fixedAreaEnd, fixedAreaSize)

		// Dump fixed area bytes
		if fixedAreaSize > 0 && fixedAreaSize < 200 {
			fixedBytes := pgBuf[rowStart+colCountSize : fixedAreaEnd+1]
			t.Logf("  fixed area hex: %x", fixedBytes)

			// Try to interpret: Id (4), ParentId (4), Type (2), DateCreate (8), DateUpdate (8), Flags (4)
			if len(fixedBytes) >= 4 {
				t.Logf("    Id (bytes 0-3 LE): %d", binary.LittleEndian.Uint32(fixedBytes[0:4]))
			}
			if len(fixedBytes) >= 8 {
				t.Logf("    ParentId (bytes 4-7 LE): %d", binary.LittleEndian.Uint32(fixedBytes[4:8]))
			}
			if len(fixedBytes) >= 10 {
				t.Logf("    Type (bytes 8-9 LE): %d", binary.LittleEndian.Uint16(fixedBytes[8:10]))
			}
			if len(fixedBytes) >= 18 {
				dateCreate := GetDouble(fixedBytes, 10)
				t.Logf("    DateCreate (bytes 10-17): %f", dateCreate)
			}
			if len(fixedBytes) >= 26 {
				dateUpdate := GetDouble(fixedBytes, 18)
				t.Logf("    DateUpdate (bytes 18-25): %f", dateUpdate)
			}
			if len(fixedBytes) >= 30 {
				t.Logf("    Flags (bytes 26-29 LE): %d (0x%x)", binary.LittleEndian.Uint32(fixedBytes[26:30]), binary.LittleEndian.Uint32(fixedBytes[26:30]))
			}
		}

		// Dump exact bytes at CrackRow-computed positions for each column
		for ci := 0; ci < int(table2.NumCols); ci++ {
			col := cols[ci]
			if col.IsFixed {
				pos := rowStart + col.FixedOffset + colCountSize
				if pos+col.ColSize <= len(pgBuf) {
					t.Logf("  CrackRow col[%d] %q fixed: pos=%d size=%d bytes=%x",
						ci, col.Name, pos, col.ColSize, pgBuf[pos:pos+col.ColSize])
				}
			} else if int(col.VarColNum) < len(varColOffsets)-1 {
				vStart := varColOffsets[col.VarColNum]
				vEnd := varColOffsets[col.VarColNum+1]
				pos := rowStart + vStart
				size := vEnd - vStart
				if pos+size <= len(pgBuf) {
					t.Logf("  CrackRow col[%d] %q var: pos=%d size=%d bytes=%x",
						ci, col.Name, pos, size, pgBuf[pos:pos+size])
				}
			}
		}
	}
}

func TestDebugMSysObjects(t *testing.T) {
	if _, err := os.Stat(testMDBPathDebug); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPathDebug)
	}

	handle, err := Open(testMDBPathDebug, MDBNoFlags)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer handle.Close()

	t.Logf("Jet version: 0x%x", handle.F.JetVersion)

	// Read MSysObjects directly
	entry := &MdbCatalogEntry{
		Mdb:        handle,
		ObjectType: MDBTable,
		TablePg:    2,
		ObjectName: "MSysObjects",
	}

	table := handle.ReadTable(entry)
	if table == nil {
		t.Fatal("ReadTable failed for MSysObjects")
	}
	defer handle.FreeTableDef(table)

	t.Logf("Table: %s, NumCols=%d, NumRows=%d, NumIdxs=%d",
		table.Name, table.NumCols, table.NumRows, table.NumRealIdxs)

	cols := handle.ReadColumns(table)
	if cols == nil {
		t.Fatal("ReadColumns returned nil")
	}

	t.Logf("Read %d columns:", len(cols))
	for i, col := range cols {
		t.Logf("  [%d] Name=%q Type=%d(%s) Size=%d Fixed=%v Num=%d",
			i, col.Name, col.ColType, ColumnTypeName(col.ColType),
			col.ColSize, col.IsFixed, col.ColNum)
	}

	// Try reading catalog
	entries := handle.ReadCatalog(MDBAny)
	t.Logf("Catalog entries: %d", len(entries))
	for i, e := range entries {
		if i >= 20 {
			t.Logf("  ... (%d more)", len(entries)-20)
			break
		}
		t.Logf("  [%d] Type=%d(%s) Name=%q Flags=0x%x Pg=%d IsUser=%v",
			i, e.ObjectType, ObjectTypeString(e.ObjectType),
			e.ObjectName, e.Flags, e.TablePg, IsUserTableEntry(e))
	}

	// Read all rows and check Name buffer state
	entry2 := &MdbCatalogEntry{
		Mdb:        handle,
		ObjectType: MDBTable,
		TablePg:    2,
		ObjectName: "MSysObjects",
	}

	table2 := handle.ReadTable(entry2)
	if table2 == nil {
		t.Fatal("ReadTable failed")
	}
	defer handle.FreeTableDef(table2)
	handle.ReadColumns(table2)

	// Bind all columns
	objNameBuf := make([]byte, handle.BindSize)
	objTypeBuf := make([]byte, handle.BindSize)
	objFlagsBuf := make([]byte, handle.BindSize)

	if handle.BindColumnByName(table2, "Id", make([]byte, handle.BindSize), nil) == -1 ||
		handle.BindColumnByName(table2, "Name", objNameBuf, nil) == -1 ||
		handle.BindColumnByName(table2, "Type", objTypeBuf, nil) == -1 ||
		handle.BindColumnByName(table2, "Flags", objFlagsBuf, nil) == -1 {
		t.Fatal("BindColumnByName failed")
	}

	table2.RewindTable()
	count2 := 0
	typeCounts2 := make(map[string]int)
	for table2.FetchRow() {
		count2++
		typeStr := strings.TrimRight(string(objTypeBuf), "\x00")
		typeCounts2[typeStr]++
		if count2 <= 8 || (count2 >= 200 && count2 <= 205) {
			nameStr := UTF16LEToString(objNameBuf)
			flagsStr := strings.TrimRight(string(objFlagsBuf), "\x00")
			t.Logf("Row %d: Type=%s Flags=%s Name=%q NameBuf[0:30]=%x",
				count2, typeStr, flagsStr, nameStr, objNameBuf[:30])
		}
	}

	// Count types
	typeCounts := make(map[int]int)
	for _, e := range entries {
		typeCounts[e.ObjectType]++
	}
	for tp, cnt := range typeCounts {
		t.Logf("  ObjectType %d (%s): %d entries", tp, ObjectTypeString(tp), cnt)
	}

	// Check IsUserTableEntry
	t.Log("=== User table entries ===")
	for i, e := range entries {
		if IsUserTableEntry(e) {
			t.Logf("  [%d] Type=%d(%s) Name=%q Flags=0x%x Pg=%d",
				i, e.ObjectType, ObjectTypeString(e.ObjectType),
				e.ObjectName, e.Flags, e.TablePg)
		}
	}

	// Test Tables() API
	t.Log("=== Tables() API ===")
	api, err := OpenMDB(testMDBPathDebug)
	if err != nil {
		t.Fatalf("OpenMDB() error = %v", err)
	}
	defer api.Close()
	tables, _ := api.Tables()
	t.Logf("Tables() returned %d tables: %v", len(tables), tables)

	names, _ := api.TableNames()
	t.Logf("TableNames() returned %d names: %v", len(names), names)

	// Test reading data from a user table
	if len(tables) > 0 {
		t.Logf("=== Reading data from first user table: %s ===", tables[0])
		rows, err := api.ReadTableData(tables[0])
		if err != nil {
			t.Logf("ReadTableData(%s) error: %v", tables[0], err)
		} else {
			t.Logf("ReadTableData(%s) returned %d rows", tables[0], len(rows))
			for i, row := range rows {
				if i >= 3 {
					t.Logf("  ... (%d more rows)", len(rows)-3)
					break
				}
				t.Logf("  Row %d: %v", i, row)
			}
		}
	}
}
