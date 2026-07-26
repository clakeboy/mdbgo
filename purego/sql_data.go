package purego

import (
	"fmt"
	"strconv"
	"strings"
)

type projectedTableData struct {
	Columns []string
	Rows    [][]string
	Nulls   [][]bool
}

func (mdb *MDB) readProjectedData(tableName string, cols []string, maxRows int) (*projectedTableData, error) {
	entry := mdb.handle.GetCatalogEntryByName(tableName)
	if entry == nil {
		return nil, fmt.Errorf("表 %s 不存在", tableName)
	}
	table := mdb.handle.ReadTable(entry)
	if table == nil {
		return nil, fmt.Errorf("无法读取表 %s", tableName)
	}
	defer mdb.handle.FreeTableDef(table)

	if mdb.handle.ReadColumns(table) == nil {
		return nil, fmt.Errorf("无法读取表 %s 的列", tableName)
	}

	requested := make([]int, 0)
	allCols := len(cols) == 0
	if !allCols {
		colSet := make(map[string]int)
		for i, c := range table.Columns {
			colSet[strings.ToLower(c.Name)] = i
		}
		for _, name := range cols {
			if idx, ok := colSet[strings.ToLower(name)]; ok {
				requested = append(requested, idx)
			}
		}
	} else {
		for i := range table.Columns {
			requested = append(requested, i)
		}
	}
	if len(requested) == 0 {
		return nil, fmt.Errorf("表 %s 中未找到请求的列", tableName)
	}

	boundValues := make([][]byte, len(requested))
	for i, idx := range requested {
		boundValues[i] = make([]byte, MDBBindSize)
		table.Columns[idx].BindPtr = boundValues[i]
	}

	outCols := make([]string, len(requested))
	for i, idx := range requested {
		outCols[i] = table.Columns[idx].Name
	}

	var outRows [][]string
	var outNulls [][]bool
	table.RewindTable()
	rowCount := 0
	for table.FetchRow() {
		if maxRows > 0 && rowCount >= maxRows {
			break
		}
		rowCount++
		row := make([]string, len(requested))
		nulls := make([]bool, len(requested))
		for i, idx := range requested {
			col := table.Columns[idx]
			if col.IsNull {
				nulls[i] = true
				row[i] = ""
				continue
			}
			buf := boundValues[i]
			if col.ColType == MDBText || col.ColType == MDBMemo {
				row[i] = UTF16LEToString(buf)
			} else {
				row[i] = strings.TrimRight(string(buf), "\x00")
			}
		}
		outRows = append(outRows, row)
		outNulls = append(outNulls, nulls)
	}

	return &projectedTableData{
		Columns: outCols,
		Rows:    outRows,
		Nulls:   outNulls,
	}, nil
}

type viewRow struct {
	Attribute  int
	Flag       int
	Name1      string
	Name2      string
	Expression string
}

func (mdb *MDB) ViewSQL(viewName string) (string, error) {
	if strings.TrimSpace(viewName) == "" {
		return "", fmt.Errorf("view name is empty")
	}

	entries := mdb.handle.ReadCatalog(MDBAny)
	var viewEntry *MdbCatalogEntry
	var msysObjectsEntry *MdbCatalogEntry
	var msysQueriesEntry *MdbCatalogEntry

	for _, e := range entries {
		if strings.EqualFold(e.ObjectName, viewName) && e.ObjectType == MDBQuery {
			viewEntry = e
		}
		if strings.EqualFold(e.ObjectName, "MSysObjects") {
			msysObjectsEntry = e
		}
		if strings.EqualFold(e.ObjectName, "MSysQueries") {
			msysQueriesEntry = e
		}
	}
	if viewEntry == nil {
		return "", fmt.Errorf("视图 %s 不存在", viewName)
	}
	if msysQueriesEntry == nil {
		return "", fmt.Errorf("MSysQueries 表不存在")
	}

	table := mdb.handle.ReadTable(msysQueriesEntry)
	if table == nil {
		return "", fmt.Errorf("无法读取 MSysQueries")
	}
	defer mdb.handle.FreeTableDef(table)
	mdb.handle.ReadColumns(table)

	attrBuf := make([]byte, MDBBindSize)
	exprBuf := make([]byte, MDBBindSize)
	flagBuf := make([]byte, MDBBindSize)
	name1Buf := make([]byte, MDBBindSize)
	name2Buf := make([]byte, MDBBindSize)
	oidBuf := make([]byte, MDBBindSize)

	if mdb.handle.BindColumnByName(table, "Attribute", attrBuf, nil) == -1 ||
		mdb.handle.BindColumnByName(table, "Expression", exprBuf, nil) == -1 ||
		mdb.handle.BindColumnByName(table, "Flag", flagBuf, nil) == -1 ||
		mdb.handle.BindColumnByName(table, "Name1", name1Buf, nil) == -1 ||
		mdb.handle.BindColumnByName(table, "Name2", name2Buf, nil) == -1 ||
		mdb.handle.BindColumnByName(table, "ObjectId", oidBuf, nil) == -1 {
		return "", fmt.Errorf("MSysQueries 列绑定失败")
	}

	viewId := int(viewEntry.TablePg)
	if msysObjectsEntry != nil {
		objTable := mdb.handle.ReadTable(msysObjectsEntry)
		if objTable != nil {
			mdb.handle.ReadColumns(objTable)
			idBuf := make([]byte, MDBBindSize)
			nameBuf := make([]byte, MDBBindSize)
			if mdb.handle.BindColumnByName(objTable, "Id", idBuf, nil) != -1 &&
				mdb.handle.BindColumnByName(objTable, "Name", nameBuf, nil) != -1 {
				objTable.RewindTable()
				for objTable.FetchRow() {
					idStr := strings.TrimRight(string(idBuf), "\x00")
					idVal, _ := strconv.Atoi(idStr)
					nameStr := UTF16LEToString(nameBuf)
					if strings.EqualFold(nameStr, viewName) {
						viewId = idVal & 0x00FFFFFF
						break
					}
				}
			}
			mdb.handle.FreeTableDef(objTable)
		}
	}

	var rows []viewRow
	table.RewindTable()
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
		flag, _ := strconv.Atoi(flagStr)
		rows = append(rows, viewRow{
			Attribute:  attr,
			Flag:       flag,
			Name1:      UTF16LEToString(name1Buf),
			Name2:      UTF16LEToString(name2Buf),
			Expression: UTF16LEToString(exprBuf),
		})
	}

	if len(rows) == 0 {
		return "", fmt.Errorf("视图 %s 的查询定义为空", viewName)
	}

	return buildViewSQL(rows, viewEntry.Flags)
}

func buildViewSQL(rows []viewRow, flags int) (string, error) {
	var sb strings.Builder

	for _, r := range rows {
		if r.Attribute == 2 {
			paramName := r.Name1
			paramType := paramTypeName(r.Flag)
			if sb.Len() > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString("[" + paramName + "] " + paramType)
		}
	}
	if sb.Len() > 0 {
		sbStr := sb.String()
		sb.Reset()
		sb.WriteString("PARAMETERS ")
		sb.WriteString(sbStr)
		sb.WriteString(";\n")
	}

	sb.WriteString("SELECT ")

	for _, r := range rows {
		if r.Attribute == 3 {
			if r.Flag&0x02 != 0 {
				sb.WriteString("DISTINCT ")
			}
			if r.Flag&0x08 != 0 {
				sb.WriteString("DISTINCTROW ")
			}
			if r.Flag&0x10 != 0 {
				sb.WriteString("TOP ")
				sb.WriteString(r.Name1)
				sb.WriteString(" ")
				if r.Flag&0x20 != 0 {
					sb.WriteString("PERCENT ")
				}
			}
		}
	}

	hasCols := false
	for _, r := range rows {
		if r.Attribute == 6 {
			if hasCols {
				sb.WriteString(", ")
			}
			expr := r.Expression
			if expr == "*" || expr == "" {
				sb.WriteString("*")
			} else {
				sb.WriteString(expr)
			}
			hasCols = true
		}
	}
	if !hasCols {
		sb.WriteString("*")
	}

	fromParts := buildFromClause(rows)
	if fromParts != "" {
		sb.WriteString("\nFROM " + fromParts)
	}

	for _, r := range rows {
		if r.Attribute == 8 && r.Expression != "" {
			sb.WriteString("\nWHERE " + r.Expression)
		}
	}

	groupExprs := make([]string, 0)
	for _, r := range rows {
		if r.Attribute == 9 && r.Expression != "" {
			groupExprs = append(groupExprs, r.Expression)
		}
	}
	if len(groupExprs) > 0 {
		sb.WriteString("\nGROUP BY " + strings.Join(groupExprs, ", "))
	}

	for _, r := range rows {
		if r.Attribute == 10 && r.Expression != "" {
			sb.WriteString("\nHAVING " + r.Expression)
		}
	}

	orderExprs := make([]string, 0)
	for _, r := range rows {
		if r.Attribute == 11 && r.Expression != "" {
			o := r.Expression
			if strings.EqualFold(r.Name1, "D") {
				o += " DESC"
			}
			orderExprs = append(orderExprs, o)
		}
	}
	if len(orderExprs) > 0 {
		sb.WriteString("\nORDER BY " + strings.Join(orderExprs, ", "))
	}

	for _, r := range rows {
		if r.Attribute == 3 && r.Flag&0x04 != 0 {
			sb.WriteString("\nWITH OWNERACCESS OPTION")
		}
	}

	sb.WriteString(";")
	return sb.String(), nil
}

func buildFromClause(rows []viewRow) string {
	type tableSource struct {
		sql  string
		keys []string
	}

	var tables []tableSource
	var joins []viewRow

	for _, r := range rows {
		if r.Attribute == 5 {
			t := tableSource{}
			// Expression 仅用于跨库表（如 "[远程库名].[表名]"）。
			// 含 * . ( 等 SQL 操作符时不作为库名前缀。
			if r.Expression != "" && !strings.ContainsAny(r.Expression, "*.") {
				t.sql = "[" + r.Expression + "].[" + r.Name1 + "]"
			} else {
				t.sql = "[" + r.Name1 + "]"
			}
			key := r.Name1
			if r.Name2 != "" {
				t.sql += " AS [" + r.Name2 + "]"
				key = r.Name2
			}
			t.keys = []string{key}
			tables = append(tables, t)
		}
		if r.Attribute == 7 {
			joins = append(joins, r)
		}
	}

	if len(tables) == 0 {
		return ""
	}

	for _, j := range joins {
		leftKey := j.Name1
		rightKey := j.Name2
		leftIdx := -1
		rightIdx := -1
		for i, t := range tables {
			for _, k := range t.keys {
				if strings.EqualFold(k, leftKey) {
					leftIdx = i
				}
				if strings.EqualFold(k, rightKey) {
					rightIdx = i
				}
			}
		}
		if leftIdx < 0 || rightIdx < 0 {
			continue
		}
		if leftIdx > rightIdx {
			leftIdx, rightIdx = rightIdx, leftIdx
		}
		joinType := "INNER JOIN"
		if j.Flag == 2 {
			joinType = "LEFT JOIN"
		} else if j.Flag == 3 {
			joinType = "RIGHT JOIN"
		}

		leftTable := tables[leftIdx].sql
		rightTable := tables[rightIdx].sql
		combined := "(" + leftTable + " " + joinType + " " + rightTable + " ON (" + j.Expression + "))"

		combinedKeys := append(tables[leftIdx].keys, tables[rightIdx].keys...)
		tables[leftIdx] = tableSource{sql: combined, keys: combinedKeys}
		tables = append(tables[:rightIdx], tables[rightIdx+1:]...)
	}

	parts := make([]string, len(tables))
	for i, t := range tables {
		parts[i] = t.sql
	}
	return strings.Join(parts, ", ")
}

func paramTypeName(flag int) string {
	switch flag {
	case 0:
		return "Value"
	case MDBBool:
		return "Bit"
	case MDBText:
		return "Text"
	case MDBByte:
		return "Byte"
	case MDBInt:
		return "Short"
	case MDBLongInt:
		return "Long"
	case MDBMoney:
		return "Currency"
	case MDBFloat:
		return "IEEESingle"
	case MDBDouble:
		return "IEEEDouble"
	case MDBDateTime:
		return "DateTime"
	case MDBBinary:
		return "Binary"
	case MDBOle:
		return "LongBinary"
	case MDBRepId:
		return "Guid"
	default:
		return fmt.Sprintf("Value(%d)", flag)
	}
}
