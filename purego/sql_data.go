package purego

import (
	"fmt"
	"strconv"
	"strings"
)

type viewRow struct {
	Attribute  int
	Flag       int
	Name1      string
	Name2      string
	Expression string
}

// relationshipColumn 保存 MSysRelationships 中一组 Access 关系字段。
type relationshipColumn struct {
	SourceColumn string
	TargetColumn string
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

	attributeIndex := mdb.handle.BindColumnByName(table, "Attribute", attrBuf, nil)
	expressionIndex := mdb.handle.BindColumnByName(table, "Expression", exprBuf, nil)
	flagIndex := mdb.handle.BindColumnByName(table, "Flag", flagBuf, nil)
	name1Index := mdb.handle.BindColumnByName(table, "Name1", name1Buf, nil)
	name2Index := mdb.handle.BindColumnByName(table, "Name2", name2Buf, nil)
	objectIDIndex := mdb.handle.BindColumnByName(table, "ObjectId", oidBuf, nil)
	if attributeIndex == -1 || expressionIndex == -1 || flagIndex == -1 ||
		name1Index == -1 || name2Index == -1 || objectIDIndex == -1 {
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
		expression, err := mdb.memoColumnString(table.Columns[expressionIndex-1])
		if err != nil {
			return "", fmt.Errorf("读取 View %s 的 Expression: %w", viewName, err)
		}
		rows = append(rows, viewRow{
			Attribute:  attr,
			Flag:       flag,
			Name1:      UTF16LEToString(name1Buf),
			Name2:      UTF16LEToString(name2Buf),
			Expression: expression,
		})
	}

	if len(rows) == 0 {
		return "", fmt.Errorf("视图 %s 的查询定义为空", viewName)
	}

	return mdb.buildViewSQL(rows, viewEntry.Flags)
}

// memoColumnString 读取当前行的完整 Memo 值；MSysQueries.Expression 经常存储在长值页中。
func (mdb *MDB) memoColumnString(column *MdbColumn) (string, error) {
	if column == nil || column.IsNull || column.CurValueLen == 0 {
		return "", nil
	}
	if column.CurValueStart < 0 || column.CurValueStart+MDBMemoOverhead > mdb.handle.Fmt.PgSize {
		return "", fmt.Errorf("Memo 指针越界")
	}
	// 长值读取器需要原始 12 字节头；普通绑定缓冲区仅保存了格式化后的字符串。
	originalBind := column.BindPtr
	header := make([]byte, MDBMemoOverhead)
	copy(header, mdb.handle.PgBuf[column.CurValueStart:column.CurValueStart+MDBMemoOverhead])
	column.BindPtr = header
	data, err := mdb.handle.ReadOleFullData(column)
	column.BindPtr = originalBind
	if err != nil {
		return "", err
	}
	return UTF16LEToString(data), nil
}

// buildViewSQL 按 Access 的 MSysQueries 定义重建可由 Go SQL 引擎执行的 SELECT 语句。
func (mdb *MDB) buildViewSQL(rows []viewRow, flags int) (string, error) {
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
		if r.Attribute == 6 && strings.TrimSpace(r.Expression) != "" {
			if hasCols {
				sb.WriteString(", ")
			}
			sb.WriteString(r.Expression)
			hasCols = true
		}
	}
	if !hasCols {
		sb.WriteString("*")
	}

	fromParts, err := mdb.buildFromClause(rows)
	if err != nil {
		return "", err
	}
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

// buildFromClause 还原表源与 JOIN；部分 Jet 数据库在 JOIN 行中只记录表名，
// 此时从两侧表结构推导唯一的同名关系字段。
func (mdb *MDB) buildFromClause(rows []viewRow) (string, error) {
	type tableSource struct {
		sql  string
		keys []string
	}

	var tables []tableSource
	var joins []viewRow
	// sourceTableNames 将查询中实际引用的名称映射回物理表名。
	// 有别名时，后续 JOIN 必须用别名生成 SQL，却要用物理表读取字段结构。
	sourceTableNames := make(map[string]string)

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
			sourceTableNames[strings.ToLower(key)] = r.Name1
			tables = append(tables, t)
		}
		if r.Attribute == 7 {
			joins = append(joins, r)
		}
	}

	if len(tables) == 0 {
		return "", nil
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
			return "", fmt.Errorf("view join references an unknown table: %s -> %s", leftKey, rightKey)
		}
		if leftIdx == rightIdx {
			return "", fmt.Errorf("view join is cyclic: %s -> %s", leftKey, rightKey)
		}
		joinType := "INNER JOIN"
		if j.Flag == 2 {
			joinType = "LEFT JOIN"
		} else if j.Flag == 3 {
			joinType = "RIGHT JOIN"
		} else if j.Flag != 1 {
			return "", fmt.Errorf("unknown Access join type: %d", j.Flag)
		}

		leftTable := tables[leftIdx].sql
		rightTable := tables[rightIdx].sql
		condition := strings.TrimSpace(j.Expression)
		if condition == "" {
			leftTableName, leftOK := sourceTableNames[strings.ToLower(leftKey)]
			rightTableName, rightOK := sourceTableNames[strings.ToLower(rightKey)]
			if !leftOK || !rightOK {
				return "", fmt.Errorf("view join source table is unavailable: %s -> %s", leftKey, rightKey)
			}
			var err error
			condition, err = mdb.inferJoinCondition(leftKey, rightKey, leftTableName, rightTableName)
			if err != nil {
				return "", err
			}
		}
		combined := "(" + leftTable + " " + joinType + " " + rightTable + " ON (" + condition + "))"

		combinedKeys := append(append([]string{}, tables[leftIdx].keys...), tables[rightIdx].keys...)
		// 保存位置与构造 JOIN 的左右顺序彼此独立，避免为删除切片元素而反转 LEFT/RIGHT JOIN 语义。
		replaceIdx, removeIdx := leftIdx, rightIdx
		if replaceIdx > removeIdx {
			replaceIdx, removeIdx = removeIdx, replaceIdx
		}
		tables[replaceIdx] = tableSource{sql: combined, keys: combinedKeys}
		tables = append(tables[:removeIdx], tables[removeIdx+1:]...)
	}

	parts := make([]string, len(tables))
	for i, t := range tables {
		parts[i] = t.sql
	}
	return strings.Join(parts, ", "), nil
}

// inferJoinCondition 为缺少 Expression 的 Jet JOIN 行推导关系字段。
// reference 名称用于输出 SQL，physical 名称用于读取实际的表结构和关系系统表。
func (mdb *MDB) inferJoinCondition(leftReference string, rightReference string, leftPhysicalTable string, rightPhysicalTable string) (string, error) {
	relationshipColumns, err := mdb.relationshipColumns(leftPhysicalTable, rightPhysicalTable)
	if err != nil {
		return "", err
	}
	if len(relationshipColumns) > 0 {
		conditions := make([]string, 0, len(relationshipColumns))
		for _, column := range relationshipColumns {
			conditions = append(conditions, leftReference+"."+column.SourceColumn+"="+rightReference+"."+column.TargetColumn)
		}
		return strings.Join(conditions, " AND "), nil
	}

	leftSchema, err := mdb.GetTableSchema(leftPhysicalTable)
	if err != nil {
		return "", fmt.Errorf("read join source table %s: %w", leftPhysicalTable, err)
	}
	rightSchema, err := mdb.GetTableSchema(rightPhysicalTable)
	if err != nil {
		return "", fmt.Errorf("read join target table %s: %w", rightPhysicalTable, err)
	}
	rightColumns := make(map[string]string, len(rightSchema.Columns))
	for _, column := range rightSchema.Columns {
		rightColumns[strings.ToLower(column.Name)] = column.Name
	}
	sharedIDColumns := make([]string, 0)
	sharedColumns := make([]string, 0)
	for _, column := range leftSchema.Columns {
		columnName := strings.TrimSpace(column.Name)
		if columnName == "" {
			continue
		}
		rightColumn, ok := rightColumns[strings.ToLower(columnName)]
		if !ok {
			continue
		}
		condition := leftReference + "." + columnName + "=" + rightReference + "." + rightColumn
		sharedColumns = append(sharedColumns, condition)
		if strings.HasSuffix(strings.ToLower(columnName), "_id") {
			sharedIDColumns = append(sharedIDColumns, condition)
		}
	}
	if len(sharedIDColumns) == 1 {
		return sharedIDColumns[0], nil
	}
	if len(sharedColumns) == 1 {
		return sharedColumns[0], nil
	}
	if len(sharedIDColumns) > 1 {
		return "", fmt.Errorf("cannot infer ambiguous join %s -> %s: %d shared ID fields", leftReference, rightReference, len(sharedIDColumns))
	}
	if condition, ok := inferSimilarJoinCondition(leftReference, rightReference, leftSchema, rightSchema); ok {
		return condition, nil
	}
	return "", fmt.Errorf("cannot infer join %s -> %s: %d shared fields", leftReference, rightReference, len(sharedColumns))
}

// inferSimilarJoinCondition 处理 Access 查询常见的“业务字段连接代码表”场景，
// 例如 bl_sort_no 与 bl_no_type_id。只有同类型且至少共享两个语义分词的唯一候选才采用。
func inferSimilarJoinCondition(leftTable string, rightTable string, leftSchema *TableSchema, rightSchema *TableSchema) (string, bool) {
	bestScore := 0
	bestMatches := 0
	bestCondition := ""
	ambiguous := false
	for _, leftColumn := range leftSchema.Columns {
		for _, rightColumn := range rightSchema.Columns {
			if leftColumn.Type != rightColumn.Type {
				continue
			}
			matches := sharedColumnTokenCount(leftColumn.Name, rightColumn.Name)
			if matches < 2 {
				continue
			}
			score := matches*10 + 1
			condition := leftTable + "." + leftColumn.Name + "=" + rightTable + "." + rightColumn.Name
			if score > bestScore {
				bestScore = score
				bestMatches = matches
				bestCondition = condition
				ambiguous = false
				continue
			}
			if score == bestScore && condition != bestCondition {
				ambiguous = true
			}
		}
	}
	if bestMatches < 2 || ambiguous {
		return "", false
	}
	return bestCondition, true
}

// sharedColumnTokenCount 按下划线分解字段名并统计共有语义词，避免仅凭通用 id/code 误匹配。
func sharedColumnTokenCount(leftName string, rightName string) int {
	leftTokens := make(map[string]struct{})
	for _, token := range strings.Split(strings.ToLower(leftName), "_") {
		if token != "" {
			leftTokens[token] = struct{}{}
		}
	}
	matches := 0
	for _, token := range strings.Split(strings.ToLower(rightName), "_") {
		if _, ok := leftTokens[token]; ok {
			matches++
		}
	}
	return matches
}

// relationshipColumns 从 SysRel 或 MSysRelationships 读取两张表之间的关系字段，
// 并统一转换为 leftTable 到 rightTable 的字段方向。
func (mdb *MDB) relationshipColumns(leftTable string, rightTable string) ([]relationshipColumn, error) {
	if mdb == nil || mdb.handle == nil {
		return nil, fmt.Errorf("database is closed")
	}
	entries := mdb.handle.ReadCatalog(MDBAny)
	var relationshipEntry *MdbCatalogEntry
	for _, entry := range entries {
		if entry != nil && strings.EqualFold(entry.ObjectName, "SysRel") {
			relationshipEntry = entry
			break
		}
	}
	if relationshipEntry == nil {
		for _, entry := range entries {
			if entry != nil && strings.EqualFold(entry.ObjectName, "MSysRelationships") {
				relationshipEntry = entry
				break
			}
		}
	}
	if relationshipEntry == nil {
		return nil, nil
	}
	table := mdb.handle.ReadTable(relationshipEntry)
	if table == nil {
		return nil, fmt.Errorf("read relationship system table")
	}
	defer mdb.handle.FreeTableDef(table)
	if mdb.handle.ReadColumns(table) == nil {
		return nil, fmt.Errorf("read relationship system table columns")
	}

	objectBuf := make([]byte, MDBBindSize)
	columnBuf := make([]byte, MDBBindSize)
	referencedObjectBuf := make([]byte, MDBBindSize)
	referencedColumnBuf := make([]byte, MDBBindSize)
	if mdb.handle.BindColumnByName(table, "szObject", objectBuf, nil) == -1 ||
		mdb.handle.BindColumnByName(table, "szColumn", columnBuf, nil) == -1 ||
		mdb.handle.BindColumnByName(table, "szReferencedObject", referencedObjectBuf, nil) == -1 ||
		mdb.handle.BindColumnByName(table, "szReferencedColumn", referencedColumnBuf, nil) == -1 {
		return nil, fmt.Errorf("bind relationship system table columns")
	}

	result := make([]relationshipColumn, 0)
	table.RewindTable()
	for table.FetchRow() {
		objectName := strings.TrimSpace(UTF16LEToString(objectBuf))
		columnName := strings.TrimSpace(UTF16LEToString(columnBuf))
		referencedObjectName := strings.TrimSpace(UTF16LEToString(referencedObjectBuf))
		referencedColumnName := strings.TrimSpace(UTF16LEToString(referencedColumnBuf))
		if objectName == "" || columnName == "" || referencedObjectName == "" || referencedColumnName == "" {
			continue
		}
		if strings.EqualFold(objectName, leftTable) && strings.EqualFold(referencedObjectName, rightTable) {
			result = append(result, relationshipColumn{SourceColumn: columnName, TargetColumn: referencedColumnName})
			continue
		}
		if strings.EqualFold(objectName, rightTable) && strings.EqualFold(referencedObjectName, leftTable) {
			result = append(result, relationshipColumn{SourceColumn: referencedColumnName, TargetColumn: columnName})
		}
	}
	return result, nil
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
