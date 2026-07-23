package mdbgo

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ValueKind identifies the logical type of a query value.
type ValueKind uint8

const (
	ValueNull ValueKind = iota
	ValueBool
	ValueInt
	ValueFloat
	ValueCurrency
	ValueString
	ValueTime
	ValueBytes
)

// Value is a typed cell returned by the Go query engine.
// Only the field matching Kind is meaningful.
type Value struct {
	Kind  ValueKind `json:"kind"`
	Bool  bool      `json:"bool,omitempty"`
	Int   int64     `json:"int,omitempty"`
	Float float64   `json:"float,omitempty"`
	Text  string    `json:"text,omitempty"`
	Time  time.Time `json:"time,omitempty"`
	Bytes []byte    `json:"bytes,omitempty"`
}

func NullValue() Value           { return Value{Kind: ValueNull} }
func BoolValue(v bool) Value     { return Value{Kind: ValueBool, Bool: v} }
func IntValue(v int64) Value     { return Value{Kind: ValueInt, Int: v} }
func FloatValue(v float64) Value { return Value{Kind: ValueFloat, Float: v} }

// CurrencyValue stores an Access Currency value as a signed integer scaled by
// 10,000 so round-trips do not lose decimal precision.
func CurrencyValue(scaled int64) Value { return Value{Kind: ValueCurrency, Int: scaled} }
func StringValue(v string) Value       { return Value{Kind: ValueString, Text: v} }
func TimeValue(v time.Time) Value      { return Value{Kind: ValueTime, Time: v} }
func BytesValue(v []byte) Value        { return Value{Kind: ValueBytes, Bytes: append([]byte(nil), v...)} }
func (v Value) IsNull() bool           { return v.Kind == ValueNull }
func (v Value) Interface() any {
	switch v.Kind {
	case ValueNull:
		return nil
	case ValueBool:
		return v.Bool
	case ValueInt:
		return v.Int
	case ValueFloat:
		return v.Float
	case ValueCurrency:
		return v.String()
	case ValueString:
		return v.Text
	case ValueTime:
		return v.Time
	case ValueBytes:
		return append([]byte(nil), v.Bytes...)
	default:
		return nil
	}
}

func (v Value) String() string {
	switch v.Kind {
	case ValueNull:
		return ""
	case ValueBool:
		if v.Bool {
			return "1"
		}
		return "0"
	case ValueInt:
		return strconv.FormatInt(v.Int, 10)
	case ValueFloat:
		return strconv.FormatFloat(v.Float, 'g', -1, 64)
	case ValueCurrency:
		sign := ""
		scaled := v.Int
		if scaled < 0 {
			sign = "-"
			scaled = -scaled
		}
		return fmt.Sprintf("%s%d.%04d", sign, scaled/10000, scaled%10000)
	case ValueString:
		return v.Text
	case ValueTime:
		return v.Time.Format("2006-01-02 15:04:05")
	case ValueBytes:
		return hex.EncodeToString(v.Bytes)
	default:
		return ""
	}
}

// ResultColumn describes one query result column.
type ResultColumn struct {
	Name     string    `json:"name"`
	Kind     ValueKind `json:"kind"`
	TypeName string    `json:"typeName,omitempty"`
}

// Rows is a materialized result with a database/sql-like iteration API.
// The execution API is intentionally compatible with a future streaming scanner.
type Rows struct {
	columns []ResultColumn
	rows    [][]Value
	pos     int
	current []Value
	err     error
	closed  bool
}

func (r *Rows) Columns() []ResultColumn {
	if r == nil {
		return nil
	}
	return append([]ResultColumn(nil), r.columns...)
}

func (r *Rows) Next() bool {
	if r == nil || r.closed || r.pos >= len(r.rows) {
		return false
	}
	r.current = r.rows[r.pos]
	r.pos++
	return true
}

func (r *Rows) Values() []Value {
	if r == nil {
		return nil
	}
	return cloneValues(r.current)
}

func (r *Rows) Scan(dest ...any) error {
	if r == nil || r.current == nil {
		return errors.New("mdbgo: Scan called without a current row")
	}
	if len(dest) != len(r.current) {
		return fmt.Errorf("mdbgo: Scan destination count %d does not match column count %d", len(dest), len(r.current))
	}
	for i := range dest {
		if err := assignValue(dest[i], r.current[i]); err != nil {
			return fmt.Errorf("mdbgo: column %d: %w", i, err)
		}
	}
	return nil
}

func (r *Rows) Err() error {
	if r == nil {
		return nil
	}
	return r.err
}

func (r *Rows) Close() error {
	if r == nil {
		return nil
	}
	r.closed = true
	r.current = nil
	return nil
}

func (r *Rows) All() [][]Value {
	if r == nil {
		return nil
	}
	out := make([][]Value, len(r.rows))
	for i := range r.rows {
		out[i] = cloneValues(r.rows[i])
	}
	return out
}

func assignValue(dest any, value Value) error {
	switch p := dest.(type) {
	case *Value:
		*p = value
	case *any:
		*p = value.Interface()
	case *string:
		*p = value.String()
	case *[]byte:
		if value.Kind == ValueBytes {
			*p = append((*p)[:0], value.Bytes...)
		} else {
			*p = append((*p)[:0], value.String()...)
		}
	case *int:
		n, ok := valueAsInt(value)
		if !ok {
			return fmt.Errorf("cannot convert %v to int", value.Kind)
		}
		*p = int(n)
	case *int64:
		n, ok := valueAsInt(value)
		if !ok {
			return fmt.Errorf("cannot convert %v to int64", value.Kind)
		}
		*p = n
	case *float64:
		n, ok := valueAsFloat(value)
		if !ok {
			return fmt.Errorf("cannot convert %v to float64", value.Kind)
		}
		*p = n
	case *bool:
		b, null := valueTruth(value)
		if null {
			return errors.New("cannot convert NULL to bool")
		}
		*p = b
	case *time.Time:
		if value.Kind != ValueTime {
			return fmt.Errorf("cannot convert %v to time.Time", value.Kind)
		}
		*p = value.Time
	default:
		return fmt.Errorf("unsupported destination %T", dest)
	}
	return nil
}

type queryRelation struct {
	columns []queryColumn
	rows    [][]Value
}

type queryColumn struct {
	name     string
	source   string
	kind     ValueKind
	typeName string
}

type queryExecutor struct {
	db         *DB
	ctx        context.Context
	params     map[string]Value
	viewStack  map[string]bool
	tableNames map[string]string
	viewNames  map[string]string
	tableCache map[string]*queryRelation
	outerRow   []Value
	outerRel   *queryRelation
}

var accessLikeCache sync.Map

var sqlASTCache = struct {
	sync.Mutex
	order  []string
	values map[string]*sqlSelect
}{values: make(map[string]*sqlSelect)}

const maxCachedSQLStatements = 256

// QueryContext parses and executes read-only Access SQL in Go.
func (db *DB) QueryContext(ctx context.Context, sqlText string, params map[string]any) (*Rows, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(sqlText) == "" {
		return nil, errors.New("sql is empty")
	}
	stmt, err := parseAccessSQLCached(sqlText)
	if err != nil {
		return nil, err
	}
	session, release, err := db.acquireQuerySession(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return session.queryStatement(ctx, stmt, params)
}

func (db *DB) queryStatement(ctx context.Context, stmt *sqlSelect, params map[string]any) (*Rows, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	values, err := normalizeParams(params)
	if err != nil {
		return nil, err
	}
	exec := &queryExecutor{
		db:         db,
		ctx:        ctx,
		params:     values,
		viewStack:  make(map[string]bool),
		tableCache: make(map[string]*queryRelation),
	}
	rel, err := exec.execute(stmt)
	if err != nil {
		return nil, err
	}
	columns := make([]ResultColumn, len(rel.columns))
	for i, col := range rel.columns {
		columns[i] = ResultColumn{Name: col.name, Kind: col.kind, TypeName: col.typeName}
	}
	return &Rows{columns: columns, rows: rel.rows}, nil
}

// QueryViewContext executes an Access saved SELECT query by name.
func (db *DB) QueryViewContext(ctx context.Context, viewName string, params map[string]any) (*Rows, error) {
	if strings.TrimSpace(viewName) == "" {
		return nil, errors.New("view name is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	session, release, err := db.acquireQuerySession(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	sqlText, err := session.ViewSQL(viewName)
	if err != nil {
		return nil, err
	}
	stmt, err := parseAccessSQLCached(sqlText)
	if err != nil {
		return nil, err
	}
	return session.queryStatement(ctx, stmt, params)
}

func normalizeParams(params map[string]any) (map[string]Value, error) {
	out := make(map[string]Value, len(params))
	for name, raw := range params {
		value, err := valueFromAny(raw)
		if err != nil {
			return nil, fmt.Errorf("parameter %q: %w", name, err)
		}
		out[strings.ToLower(strings.Trim(name, "[]"))] = value
	}
	return out, nil
}

func valueFromAny(raw any) (Value, error) {
	switch v := raw.(type) {
	case nil:
		return NullValue(), nil
	case Value:
		return v, nil
	case bool:
		return BoolValue(v), nil
	case int:
		return IntValue(int64(v)), nil
	case int8:
		return IntValue(int64(v)), nil
	case int16:
		return IntValue(int64(v)), nil
	case int32:
		return IntValue(int64(v)), nil
	case int64:
		return IntValue(v), nil
	case uint:
		return IntValue(int64(v)), nil
	case uint8:
		return IntValue(int64(v)), nil
	case uint16:
		return IntValue(int64(v)), nil
	case uint32:
		return IntValue(int64(v)), nil
	case uint64:
		if v > math.MaxInt64 {
			return Value{}, errors.New("uint64 overflows int64")
		}
		return IntValue(int64(v)), nil
	case float32:
		return FloatValue(float64(v)), nil
	case float64:
		return FloatValue(v), nil
	case string:
		return StringValue(v), nil
	case []byte:
		return BytesValue(v), nil
	case time.Time:
		return TimeValue(v), nil
	default:
		return Value{}, fmt.Errorf("unsupported value type %T", raw)
	}
}

func (e *queryExecutor) execute(stmt *sqlSelect) (*queryRelation, error) {
	if err := e.ctx.Err(); err != nil {
		return nil, err
	}
	scanLimit := simpleScanLimit(stmt)
	rel, err := e.executeSource(stmt, stmt.source, scanLimit)
	if err != nil {
		return nil, err
	}
	if stmt.where != nil {
		filtered := rel.rows[:0]
		for i, row := range rel.rows {
			if i&1023 == 0 {
				if err := e.ctx.Err(); err != nil {
					return nil, err
				}
			}
			v, err := e.eval(stmt.where, row, rel, nil)
			if err != nil {
				return nil, fmt.Errorf("WHERE: %w", err)
			}
			ok, null := valueTruth(v)
			if !null && ok {
				filtered = append(filtered, row)
			}
		}
		rel.rows = filtered
	}
	aggregate := statementHasAggregate(stmt)
	if len(stmt.order) > 0 && !aggregate {
		orders := resolveOrderAliases(stmt.order, stmt.items)
		if err := e.sortResult(orders, rel); err != nil {
			return nil, err
		}
	}
	projected, err := e.project(stmt, rel)
	if err != nil {
		return nil, err
	}
	if stmt.distinct || stmt.distinctRow {
		projected.rows = distinctRows(projected.rows)
	}
	if len(stmt.order) > 0 && aggregate {
		if err := e.sortResult(stmt.order, projected); err != nil {
			return nil, err
		}
	}
	if stmt.top >= 0 {
		n := stmt.top
		if stmt.topPercent {
			n = int(math.Ceil(float64(len(projected.rows)) * float64(stmt.top) / 100))
		}
		if n < len(projected.rows) {
			projected.rows = projected.rows[:n]
		}
	}
	start := stmt.offset
	if start > len(projected.rows) {
		start = len(projected.rows)
	}
	end := len(projected.rows)
	if stmt.limit >= 0 && start+stmt.limit < end {
		end = start + stmt.limit
	}
	projected.rows = projected.rows[start:end]
	if stmt.union != nil {
		right, err := e.execute(stmt.union)
		if err != nil {
			return nil, err
		}
		if len(projected.columns) != len(right.columns) {
			return nil, fmt.Errorf("UNION column count mismatch: %d and %d", len(projected.columns), len(right.columns))
		}
		projected.rows = append(projected.rows, right.rows...)
		if !stmt.unionAll {
			projected.rows = distinctRows(projected.rows)
		}
		// Access places the final UNION ORDER BY on the last SELECT. Apply it
		// again to the combined relation rather than leaving only the right
		// branch sorted.
		if len(stmt.union.order) > 0 {
			if err := e.sortResult(stmt.union.order, projected); err != nil {
				return nil, fmt.Errorf("UNION ORDER BY: %w", err)
			}
		}
	}
	return projected, nil
}

func (e *queryExecutor) executeSource(stmt *sqlSelect, source sqlSource, scanLimit int) (*queryRelation, error) {
	switch x := source.(type) {
	case sqlTableSource:
		return e.loadSource(x.ref, scanLimit, requiredColumnsFor(stmt, x.ref))
	case sqlJoinSource:
		left, err := e.executeSource(stmt, x.left, 0)
		if err != nil {
			return nil, err
		}
		right, err := e.executeSource(stmt, x.right, 0)
		if err != nil {
			return nil, err
		}
		return e.join(left, right, sqlJoin{kind: x.kind, on: x.on})
	default:
		return nil, errors.New("query has no FROM source")
	}
}

func (e *queryExecutor) catalog() error {
	if e.tableNames != nil {
		return nil
	}
	tables, err := e.db.Tables()
	if err != nil {
		return err
	}
	views, err := e.db.Views()
	if err != nil {
		return err
	}
	_ = tables
	_ = views
	e.db.metaMu.Lock()
	e.tableNames = e.db.tableLookup
	e.viewNames = e.db.viewLookup
	e.db.metaMu.Unlock()
	return nil
}

func (e *queryExecutor) loadSource(ref sqlTableRef, maxRows int, requestedColumns []string) (*queryRelation, error) {
	if err := e.catalog(); err != nil {
		return nil, err
	}
	key := strings.ToLower(ref.name)
	if name, ok := e.tableNames[key]; ok {
		return e.loadPhysicalTable(name, ref.alias, maxRows, requestedColumns)
	}
	if name, ok := e.viewNames[key]; ok {
		if e.viewStack[key] {
			return nil, fmt.Errorf("cyclic saved query reference: %s", ref.name)
		}
		e.viewStack[key] = true
		defer delete(e.viewStack, key)
		sqlText, err := e.db.ViewSQL(name)
		if err != nil {
			return nil, err
		}
		stmt, err := parseAccessSQLCached(sqlText)
		if err != nil {
			return nil, fmt.Errorf("saved query %q: %w", name, err)
		}
		rel, err := e.execute(stmt)
		if err != nil {
			return nil, fmt.Errorf("saved query %q: %w", name, err)
		}
		source := ref.alias
		if source == "" {
			source = ref.name
		}
		for i := range rel.columns {
			rel.columns[i].source = source
		}
		return rel, nil
	}
	return nil, fmt.Errorf("%s is not a table or saved query in this database", ref.name)
}

func parseAccessSQLCached(sqlText string) (*sqlSelect, error) {
	sqlASTCache.Lock()
	if cached := sqlASTCache.values[sqlText]; cached != nil {
		sqlASTCache.Unlock()
		return cached, nil
	}
	sqlASTCache.Unlock()
	stmt, err := parseAccessSQL(sqlText)
	if err != nil {
		return nil, err
	}
	sqlASTCache.Lock()
	defer sqlASTCache.Unlock()
	if cached := sqlASTCache.values[sqlText]; cached != nil {
		return cached, nil
	}
	if len(sqlASTCache.order) >= maxCachedSQLStatements {
		delete(sqlASTCache.values, sqlASTCache.order[0])
		copy(sqlASTCache.order, sqlASTCache.order[1:])
		sqlASTCache.order = sqlASTCache.order[:len(sqlASTCache.order)-1]
	}
	sqlASTCache.values[sqlText] = stmt
	sqlASTCache.order = append(sqlASTCache.order, sqlText)
	return stmt, nil
}

func (e *queryExecutor) loadPhysicalTable(name, alias string, maxRows int, requestedColumns []string) (*queryRelation, error) {
	schema, err := e.db.Schema(name)
	if err != nil {
		return nil, err
	}
	requestedColumns = filterRequestedColumns(requestedColumns, schema)
	cacheKey := fmt.Sprintf("%s\x00%d\x00%s", strings.ToLower(name), maxRows, strings.Join(requestedColumns, "\x00"))
	if cached, ok := e.tableCache[cacheKey]; ok {
		source := alias
		if source == "" {
			source = name
		}
		rel := &queryRelation{
			columns: append([]queryColumn(nil), cached.columns...),
			rows:    cached.rows,
		}
		for i := range rel.columns {
			rel.columns[i].source = source
		}
		return rel, nil
	}
	data, err := e.db.readTableBatched(e.ctx, name, requestedColumns, maxRows)
	if err != nil {
		return nil, err
	}
	source := alias
	if source == "" {
		source = name
	}
	rel := &queryRelation{
		columns: make([]queryColumn, len(data.Columns)),
		rows:    make([][]Value, len(data.Rows)),
	}
	for i, col := range data.Columns {
		kind := ValueString
		typeName := ""
		for _, schemaCol := range schema.Columns {
			if strings.EqualFold(schemaCol.Name, col) {
				kind = valueKindForAccessType(schemaCol.ColType)
				typeName = schemaCol.TypeName
				break
			}
		}
		rel.columns[i] = queryColumn{name: col, source: source, kind: kind, typeName: typeName}
	}
	for i, rawRow := range data.Rows {
		if i&1023 == 0 {
			if err := e.ctx.Err(); err != nil {
				return nil, err
			}
		}
		row := make([]Value, len(rel.columns))
		for j := range row {
			raw := ""
			if j < len(rawRow) {
				raw = rawRow[j]
			}
			if i < len(data.Nulls) && j < len(data.Nulls[i]) && data.Nulls[i][j] {
				row[j] = NullValue()
			} else {
				row[j] = parseAccessValue(raw, rel.columns[j].kind)
			}
		}
		rel.rows[i] = row
	}
	cacheColumns := append([]queryColumn(nil), rel.columns...)
	for i := range cacheColumns {
		cacheColumns[i].source = name
	}
	e.tableCache[cacheKey] = &queryRelation{columns: cacheColumns, rows: rel.rows}
	return rel, nil
}

func valueKindForAccessType(colType int) ValueKind {
	switch colType {
	case 0x01:
		return ValueBool
	case 0x02, 0x03, 0x04:
		return ValueInt
	case 0x05:
		return ValueCurrency
	case 0x06, 0x07, 0x10:
		return ValueFloat
	case 0x08:
		return ValueTime
	case 0x09, 0x0b:
		return ValueBytes
	default:
		return ValueString
	}
}

func parseAccessValue(raw string, kind ValueKind) Value {
	switch kind {
	case ValueBool:
		return BoolValue(raw == "1" || strings.EqualFold(raw, "true") || strings.EqualFold(raw, "yes"))
	case ValueInt:
		if n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil {
			return IntValue(n)
		}
	case ValueFloat:
		if n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil {
			return FloatValue(n)
		}
	case ValueCurrency:
		if n, ok := parseCurrency(raw); ok {
			return CurrencyValue(n)
		}
	case ValueTime:
		for _, layout := range []string{
			"2006-01-02 15:04:05", "01/02/06 15:04:05", "01/02/2006 15:04:05",
			time.RFC3339, "2006-01-02",
		} {
			if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
				return TimeValue(t)
			}
		}
	case ValueBytes:
		return BytesValue([]byte(raw))
	}
	return StringValue(raw)
}

func parseCurrency(raw string) (int64, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return 0, false
	}
	sign := int64(1)
	if text[0] == '-' {
		sign = -1
		text = text[1:]
	} else if text[0] == '+' {
		text = text[1:]
	}
	parts := strings.SplitN(text, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, false
	}
	fracText := ""
	if len(parts) == 2 {
		fracText = parts[1]
	}
	if len(fracText) > 4 {
		fracText = fracText[:4]
	}
	fracText += strings.Repeat("0", 4-len(fracText))
	frac := int64(0)
	if fracText != "" {
		frac, err = strconv.ParseInt(fracText, 10, 64)
		if err != nil {
			return 0, false
		}
	}
	return sign * (whole*10000 + frac), true
}

func (e *queryExecutor) join(left, right *queryRelation, join sqlJoin) (*queryRelation, error) {
	if join.kind != "CROSS" {
		if li, ri, ok := hashJoinColumns(join.on, left, right); ok {
			return e.hashJoin(left, right, join, li, ri)
		}
	}
	out := &queryRelation{columns: append(append([]queryColumn(nil), left.columns...), right.columns...)}
	rightMatched := make([]bool, len(right.rows))
	nullLeft := make([]Value, len(left.columns))
	nullRight := make([]Value, len(right.columns))
	for i := range nullLeft {
		nullLeft[i] = NullValue()
	}
	for i := range nullRight {
		nullRight[i] = NullValue()
	}
	for li, lrow := range left.rows {
		if li&255 == 0 {
			if err := e.ctx.Err(); err != nil {
				return nil, err
			}
		}
		matched := false
		for ri, rrow := range right.rows {
			row := append(append(make([]Value, 0, len(out.columns)), lrow...), rrow...)
			ok := join.kind == "CROSS"
			if !ok {
				v, err := e.eval(join.on, row, out, nil)
				if err != nil {
					return nil, fmt.Errorf("%s JOIN: %w", join.kind, err)
				}
				ok, _ = valueTruth(v)
			}
			if ok {
				out.rows = append(out.rows, row)
				matched = true
				rightMatched[ri] = true
			}
		}
		if !matched && join.kind == "LEFT" {
			row := append(append(make([]Value, 0, len(out.columns)), lrow...), nullRight...)
			out.rows = append(out.rows, row)
		}
	}
	if join.kind == "RIGHT" {
		for ri, matched := range rightMatched {
			if !matched {
				row := append(append(make([]Value, 0, len(out.columns)), nullLeft...), right.rows[ri]...)
				out.rows = append(out.rows, row)
			}
		}
	}
	return out, nil
}

func hashJoinColumns(expr sqlExpr, left, right *queryRelation) (int, int, bool) {
	binary, ok := expr.(sqlBinary)
	if !ok || binary.op != "=" {
		return 0, 0, false
	}
	lident, lok := binary.left.(sqlIdent)
	rident, rok := binary.right.(sqlIdent)
	if !lok || !rok {
		return 0, 0, false
	}
	if li, ok := relationColumnIndex(left, lident); ok {
		if ri, ok := relationColumnIndex(right, rident); ok {
			return li, ri, true
		}
	}
	if li, ok := relationColumnIndex(left, rident); ok {
		if ri, ok := relationColumnIndex(right, lident); ok {
			return li, ri, true
		}
	}
	return 0, 0, false
}

func relationColumnIndex(rel *queryRelation, ident sqlIdent) (int, bool) {
	name := ident.parts[len(ident.parts)-1]
	source := ""
	if len(ident.parts) > 1 {
		source = ident.parts[len(ident.parts)-2]
	}
	found := -1
	for i, col := range rel.columns {
		if strings.EqualFold(col.name, name) && (source == "" || strings.EqualFold(col.source, source)) {
			if found >= 0 {
				return 0, false
			}
			found = i
		}
	}
	return found, found >= 0
}

func (e *queryExecutor) hashJoin(left, right *queryRelation, join sqlJoin, leftKey, rightKey int) (*queryRelation, error) {
	out := &queryRelation{columns: append(append([]queryColumn(nil), left.columns...), right.columns...)}
	nullLeft := make([]Value, len(left.columns))
	nullRight := make([]Value, len(right.columns))
	for i := range nullLeft {
		nullLeft[i] = NullValue()
	}
	for i := range nullRight {
		nullRight[i] = NullValue()
	}

	// Build on the side that preserves the required outer-join ordering.
	if join.kind == "RIGHT" {
		buckets := make(map[string][][]Value, len(left.rows))
		for _, row := range left.rows {
			if row[leftKey].IsNull() {
				continue
			}
			buckets[rowKey([]Value{row[leftKey]})] = append(buckets[rowKey([]Value{row[leftKey]})], row)
		}
		for i, rrow := range right.rows {
			if i&1023 == 0 {
				if err := e.ctx.Err(); err != nil {
					return nil, err
				}
			}
			matches := buckets[rowKey([]Value{rrow[rightKey]})]
			if rrow[rightKey].IsNull() || len(matches) == 0 {
				out.rows = append(out.rows, append(append(make([]Value, 0, len(out.columns)), nullLeft...), rrow...))
				continue
			}
			for _, lrow := range matches {
				out.rows = append(out.rows, append(append(make([]Value, 0, len(out.columns)), lrow...), rrow...))
			}
		}
		return out, nil
	}

	buckets := make(map[string][][]Value, len(right.rows))
	for _, row := range right.rows {
		if row[rightKey].IsNull() {
			continue
		}
		key := rowKey([]Value{row[rightKey]})
		buckets[key] = append(buckets[key], row)
	}
	for i, lrow := range left.rows {
		if i&1023 == 0 {
			if err := e.ctx.Err(); err != nil {
				return nil, err
			}
		}
		var matches [][]Value
		if !lrow[leftKey].IsNull() {
			matches = buckets[rowKey([]Value{lrow[leftKey]})]
		}
		if len(matches) == 0 {
			if join.kind == "LEFT" {
				out.rows = append(out.rows, append(append(make([]Value, 0, len(out.columns)), lrow...), nullRight...))
			}
			continue
		}
		for _, rrow := range matches {
			out.rows = append(out.rows, append(append(make([]Value, 0, len(out.columns)), lrow...), rrow...))
		}
	}
	return out, nil
}

func (e *queryExecutor) project(stmt *sqlSelect, source *queryRelation) (*queryRelation, error) {
	aggregate := statementHasAggregate(stmt)
	result := &queryRelation{}
	if !aggregate {
		result.columns = projectedColumns(stmt.items, source)
		for i, row := range source.rows {
			if i&1023 == 0 {
				if err := e.ctx.Err(); err != nil {
					return nil, err
				}
			}
			values, err := e.projectRow(stmt.items, row, source, nil)
			if err != nil {
				return nil, err
			}
			result.rows = append(result.rows, values)
		}
		inferColumnKinds(result)
		return result, nil
	}

	groups := make(map[string][][]Value)
	var order []string
	if len(stmt.group) == 0 {
		groups[""] = source.rows
		order = append(order, "")
	} else {
		for _, row := range source.rows {
			keyValues := make([]Value, len(stmt.group))
			for i, expr := range stmt.group {
				v, err := e.eval(expr, row, source, nil)
				if err != nil {
					return nil, fmt.Errorf("GROUP BY: %w", err)
				}
				keyValues[i] = v
			}
			key := rowKey(keyValues)
			if _, ok := groups[key]; !ok {
				order = append(order, key)
			}
			groups[key] = append(groups[key], row)
		}
	}
	result.columns = projectedColumns(stmt.items, source)
	for _, key := range order {
		rows := groups[key]
		var sample []Value
		if len(rows) > 0 {
			sample = rows[0]
		} else {
			sample = make([]Value, len(source.columns))
			for i := range sample {
				sample[i] = NullValue()
			}
		}
		if stmt.having != nil {
			v, err := e.eval(stmt.having, sample, source, rows)
			if err != nil {
				return nil, fmt.Errorf("HAVING: %w", err)
			}
			ok, null := valueTruth(v)
			if null || !ok {
				continue
			}
		}
		values, err := e.projectRow(stmt.items, sample, source, rows)
		if err != nil {
			return nil, err
		}
		result.rows = append(result.rows, values)
	}
	inferColumnKinds(result)
	return result, nil
}

func projectedColumns(items []sqlSelectItem, source *queryRelation) []queryColumn {
	var result []queryColumn
	for _, item := range items {
		if star, ok := item.expr.(sqlStar); ok {
			for _, col := range source.columns {
				if star.qualifier == "" || strings.EqualFold(star.qualifier, col.source) {
					result = append(result, queryColumn{name: col.name, kind: col.kind, typeName: col.typeName})
				}
			}
			continue
		}
		name := item.alias
		if name == "" {
			name = expressionName(item.expr)
		}
		col := queryColumn{name: name}
		if ident, ok := item.expr.(sqlIdent); ok && len(ident.parts) > 1 {
			col.source = ident.parts[len(ident.parts)-2]
		}
		result = append(result, col)
	}
	return result
}

func (e *queryExecutor) projectRow(items []sqlSelectItem, row []Value, source *queryRelation, group [][]Value) ([]Value, error) {
	var result []Value
	for _, item := range items {
		if star, ok := item.expr.(sqlStar); ok {
			for i, col := range source.columns {
				if star.qualifier == "" || strings.EqualFold(star.qualifier, col.source) {
					result = append(result, row[i])
				}
			}
			continue
		}
		v, err := e.eval(item.expr, row, source, group)
		if err != nil {
			return nil, fmt.Errorf("SELECT %s: %w", expressionName(item.expr), err)
		}
		result = append(result, v)
	}
	return result, nil
}

func inferColumnKinds(rel *queryRelation) {
	for i := range rel.columns {
		for _, row := range rel.rows {
			if i < len(row) && !row[i].IsNull() {
				rel.columns[i].kind = row[i].Kind
				break
			}
		}
	}
}

func (e *queryExecutor) sortResult(order []sqlOrder, rel *queryRelation) error {
	type orderKey struct {
		row  []Value
		keys []Value
	}
	keyed := make([]orderKey, len(rel.rows))
	for i, row := range rel.rows {
		keyed[i].row = row
		keyed[i].keys = make([]Value, len(order))
		for j, ord := range order {
			v, err := e.eval(ord.expr, row, rel, nil)
			if err != nil {
				return fmt.Errorf("ORDER BY: %w", err)
			}
			keyed[i].keys[j] = v
		}
	}
	sort.SliceStable(keyed, func(i, j int) bool {
		for k, ord := range order {
			cmp := compareValues(keyed[i].keys[k], keyed[j].keys[k])
			if cmp == 0 {
				continue
			}
			if ord.desc {
				return cmp > 0
			}
			return cmp < 0
		}
		return false
	})
	for i := range keyed {
		rel.rows[i] = keyed[i].row
	}
	return nil
}

func (e *queryExecutor) eval(expr sqlExpr, row []Value, rel *queryRelation, group [][]Value) (Value, error) {
	switch x := expr.(type) {
	case nil:
		return NullValue(), nil
	case sqlLiteral:
		return x.value, nil
	case sqlIdent:
		return e.resolveIdent(x, row, rel)
	case sqlUnary:
		v, err := e.eval(x.expr, row, rel, group)
		if err != nil {
			return Value{}, err
		}
		switch strings.ToUpper(x.op) {
		case "NOT":
			b, null := valueTruth(v)
			if null {
				return NullValue(), nil
			}
			return BoolValue(!b), nil
		case "-":
			if n, ok := valueAsFloat(v); ok {
				if v.Kind == ValueInt {
					return IntValue(-v.Int), nil
				}
				return FloatValue(-n), nil
			}
		case "+":
			return v, nil
		}
		return Value{}, fmt.Errorf("invalid unary %s operand", x.op)
	case sqlBinary:
		left, err := e.eval(x.left, row, rel, group)
		if err != nil {
			return Value{}, err
		}
		right, err := e.eval(x.right, row, rel, group)
		if err != nil {
			return Value{}, err
		}
		return evalBinary(x.op, left, right)
	case sqlIn:
		value, err := e.eval(x.expr, row, rel, group)
		if err != nil {
			return Value{}, err
		}
		if value.IsNull() {
			return NullValue(), nil
		}
		found := false
		hasNull := false
		if x.query != nil {
			sub, err := e.executeSubquery(x.query, row, rel)
			if err != nil {
				return Value{}, err
			}
			if len(sub.columns) != 1 {
				return Value{}, fmt.Errorf("IN subquery must return one column, got %d", len(sub.columns))
			}
			for _, subRow := range sub.rows {
				v := subRow[0]
				if v.IsNull() {
					hasNull = true
				} else if compareValues(value, v) == 0 {
					found = true
					break
				}
			}
			if !found && hasNull {
				return NullValue(), nil
			}
			if x.not {
				found = !found
			}
			return BoolValue(found), nil
		}
		for _, item := range x.list {
			v, err := e.eval(item, row, rel, group)
			if err != nil {
				return Value{}, err
			}
			if v.IsNull() {
				hasNull = true
			} else if compareValues(value, v) == 0 {
				found = true
				break
			}
		}
		if !found && hasNull {
			return NullValue(), nil
		}
		if x.not {
			found = !found
		}
		return BoolValue(found), nil
	case sqlBetween:
		value, err := e.eval(x.expr, row, rel, group)
		if err != nil {
			return Value{}, err
		}
		low, err := e.eval(x.low, row, rel, group)
		if err != nil {
			return Value{}, err
		}
		high, err := e.eval(x.high, row, rel, group)
		if err != nil {
			return Value{}, err
		}
		if value.IsNull() || low.IsNull() || high.IsNull() {
			return NullValue(), nil
		}
		ok := compareValues(value, low) >= 0 && compareValues(value, high) <= 0
		if x.not {
			ok = !ok
		}
		return BoolValue(ok), nil
	case sqlCall:
		return e.evalCall(x, row, rel, group)
	case sqlSubquery:
		sub, err := e.executeSubquery(x.stmt, row, rel)
		if err != nil {
			return Value{}, err
		}
		if x.exists {
			return BoolValue(len(sub.rows) > 0), nil
		}
		if len(sub.columns) != 1 {
			return Value{}, fmt.Errorf("scalar subquery must return one column, got %d", len(sub.columns))
		}
		if len(sub.rows) == 0 {
			return NullValue(), nil
		}
		if len(sub.rows) > 1 {
			return Value{}, errors.New("scalar subquery returned more than one row")
		}
		return sub.rows[0][0], nil
	default:
		return Value{}, fmt.Errorf("unsupported expression %T", expr)
	}
}

func (e *queryExecutor) resolveIdent(ident sqlIdent, row []Value, rel *queryRelation) (Value, error) {
	name := ident.parts[len(ident.parts)-1]
	source := ""
	if len(ident.parts) > 1 {
		source = ident.parts[len(ident.parts)-2]
	}
	found := -1
	for i, col := range rel.columns {
		if strings.EqualFold(col.name, name) && (source == "" || strings.EqualFold(col.source, source)) {
			if found >= 0 {
				return Value{}, fmt.Errorf("ambiguous column %s", strings.Join(ident.parts, "."))
			}
			found = i
		}
	}
	if found >= 0 {
		if found >= len(row) {
			return NullValue(), nil
		}
		return row[found], nil
	}
	if source == "" {
		if value, ok := e.params[strings.ToLower(name)]; ok {
			return value, nil
		}
	}
	if e.outerRel != nil {
		outer := *e
		outer.outerRel = nil
		outer.outerRow = nil
		if value, err := outer.resolveIdent(ident, e.outerRow, e.outerRel); err == nil {
			return value, nil
		}
	}
	return Value{}, fmt.Errorf("column or parameter %s not found", strings.Join(ident.parts, "."))
}

func (e *queryExecutor) executeSubquery(stmt *sqlSelect, outerRow []Value, outerRel *queryRelation) (*queryRelation, error) {
	sub := *e
	sub.outerRow = outerRow
	sub.outerRel = outerRel
	return sub.execute(stmt)
}

func (e *queryExecutor) evalCall(call sqlCall, row []Value, rel *queryRelation, group [][]Value) (Value, error) {
	name := strings.ToUpper(call.name)
	if isAggregateName(name) {
		if group == nil {
			group = [][]Value{row}
		}
		return e.evalAggregate(name, call.args, group, rel)
	}
	args := make([]Value, len(call.args))
	for i := range call.args {
		v, err := e.eval(call.args[i], row, rel, group)
		if err != nil {
			return Value{}, err
		}
		args[i] = v
	}
	switch name {
	case "IIF":
		if len(args) != 3 {
			return Value{}, errors.New("IIf requires 3 arguments")
		}
		ok, null := valueTruth(args[0])
		if !null && ok {
			return args[1], nil
		}
		return args[2], nil
	case "NZ":
		if len(args) < 1 || len(args) > 2 {
			return Value{}, errors.New("Nz requires 1 or 2 arguments")
		}
		if !args[0].IsNull() {
			return args[0], nil
		}
		if len(args) == 2 {
			return args[1], nil
		}
		return StringValue(""), nil
	case "LEN":
		return unaryStringFunction(name, args, func(s string) Value { return IntValue(int64(len([]rune(s)))) })
	case "LCASE":
		return unaryStringFunction(name, args, func(s string) Value { return StringValue(strings.ToLower(s)) })
	case "UCASE":
		return unaryStringFunction(name, args, func(s string) Value { return StringValue(strings.ToUpper(s)) })
	case "TRIM":
		return unaryStringFunction(name, args, func(s string) Value { return StringValue(strings.TrimSpace(s)) })
	case "LTRIM":
		return unaryStringFunction(name, args, func(s string) Value { return StringValue(strings.TrimLeft(s, " \t\r\n")) })
	case "RTRIM":
		return unaryStringFunction(name, args, func(s string) Value { return StringValue(strings.TrimRight(s, " \t\r\n")) })
	case "LEFT", "RIGHT":
		if len(args) != 2 {
			return Value{}, fmt.Errorf("%s requires 2 arguments", name)
		}
		n, ok := valueAsInt(args[1])
		if !ok || n < 0 {
			return Value{}, fmt.Errorf("%s length must be non-negative", name)
		}
		runes := []rune(args[0].String())
		if int(n) > len(runes) {
			n = int64(len(runes))
		}
		if name == "LEFT" {
			return StringValue(string(runes[:n])), nil
		}
		return StringValue(string(runes[len(runes)-int(n):])), nil
	case "MID":
		if len(args) < 2 || len(args) > 3 {
			return Value{}, errors.New("Mid requires 2 or 3 arguments")
		}
		start, ok := valueAsInt(args[1])
		if !ok || start < 1 {
			return Value{}, errors.New("Mid start must be >= 1")
		}
		runes := []rune(args[0].String())
		begin := int(start - 1)
		if begin > len(runes) {
			begin = len(runes)
		}
		end := len(runes)
		if len(args) == 3 {
			n, ok := valueAsInt(args[2])
			if !ok || n < 0 {
				return Value{}, errors.New("Mid length must be non-negative")
			}
			if begin+int(n) < end {
				end = begin + int(n)
			}
		}
		return StringValue(string(runes[begin:end])), nil
	case "CSTR":
		if len(args) != 1 {
			return Value{}, errors.New("CStr requires 1 argument")
		}
		if args[0].IsNull() {
			return NullValue(), nil
		}
		return StringValue(args[0].String()), nil
	case "CLNG", "CINT":
		if len(args) != 1 {
			return Value{}, fmt.Errorf("%s requires 1 argument", name)
		}
		n, ok := valueAsFloat(args[0])
		if !ok {
			return Value{}, fmt.Errorf("%s cannot convert value", name)
		}
		return IntValue(int64(math.Round(n))), nil
	case "CDBL", "CSNG":
		if len(args) != 1 {
			return Value{}, fmt.Errorf("%s requires 1 argument", name)
		}
		n, ok := valueAsFloat(args[0])
		if !ok {
			return Value{}, fmt.Errorf("%s cannot convert value", name)
		}
		return FloatValue(n), nil
	case "DATE":
		if len(args) != 0 {
			return Value{}, errors.New("Date requires no arguments")
		}
		now := time.Now()
		return TimeValue(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())), nil
	case "NOW":
		if len(args) != 0 {
			return Value{}, errors.New("Now requires no arguments")
		}
		return TimeValue(time.Now()), nil
	case "DATEVALUE":
		if len(args) != 1 {
			return Value{}, errors.New("DateValue requires 1 argument")
		}
		if args[0].Kind == ValueTime {
			t := args[0].Time
			return TimeValue(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())), nil
		}
		t, err := parseAccessDate(args[0].String())
		if err != nil {
			return Value{}, err
		}
		return TimeValue(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())), nil
	case "DATEPART":
		if len(args) < 2 || len(args) > 4 {
			return Value{}, errors.New("DatePart requires 2 to 4 arguments")
		}
		if args[1].IsNull() {
			return NullValue(), nil
		}
		var date time.Time
		if args[1].Kind == ValueTime {
			date = args[1].Time
		} else {
			var err error
			date, err = parseAccessDate(args[1].String())
			if err != nil {
				return Value{}, err
			}
		}
		switch strings.ToLower(args[0].String()) {
		case "yyyy":
			return IntValue(int64(date.Year())), nil
		case "q":
			return IntValue(int64((int(date.Month())-1)/3 + 1)), nil
		case "m":
			return IntValue(int64(date.Month())), nil
		case "y":
			return IntValue(int64(date.YearDay())), nil
		case "d":
			return IntValue(int64(date.Day())), nil
		case "w":
			return IntValue(int64(date.Weekday()) + 1), nil
		case "ww":
			_, week := date.ISOWeek()
			return IntValue(int64(week)), nil
		case "h":
			return IntValue(int64(date.Hour())), nil
		case "n":
			return IntValue(int64(date.Minute())), nil
		case "s":
			return IntValue(int64(date.Second())), nil
		default:
			return Value{}, fmt.Errorf("unsupported DatePart interval %q", args[0].String())
		}
	case "FORMAT":
		if len(args) < 2 || len(args) > 4 {
			return Value{}, errors.New("Format requires 2 to 4 arguments")
		}
		if args[0].IsNull() {
			return NullValue(), nil
		}
		format := args[1].String()
		if args[0].Kind == ValueTime {
			replacer := strings.NewReplacer(
				"yyyy", "2006", "yy", "06", "mmmm", "January", "mmm", "Jan",
				"mm", "01", "dd", "02", "hh", "15", "nn", "04", "ss", "05",
			)
			return StringValue(args[0].Time.Format(replacer.Replace(strings.ToLower(format)))), nil
		}
		switch strings.ToLower(format) {
		case "general number", "general":
			return StringValue(args[0].String()), nil
		case "fixed":
			n, ok := valueAsFloat(args[0])
			if !ok {
				return Value{}, errors.New("Format Fixed requires a number")
			}
			return StringValue(strconv.FormatFloat(n, 'f', 2, 64)), nil
		default:
			return StringValue(args[0].String()), nil
		}
	case "ABS":
		if len(args) != 1 {
			return Value{}, errors.New("Abs requires 1 argument")
		}
		if args[0].Kind == ValueInt {
			if args[0].Int < 0 {
				return IntValue(-args[0].Int), nil
			}
			return args[0], nil
		}
		if args[0].Kind == ValueCurrency {
			if args[0].Int < 0 {
				return CurrencyValue(-args[0].Int), nil
			}
			return args[0], nil
		}
		n, ok := valueAsFloat(args[0])
		if !ok {
			return Value{}, errors.New("Abs requires a number")
		}
		return FloatValue(math.Abs(n)), nil
	case "ROUND":
		if len(args) < 1 || len(args) > 2 {
			return Value{}, errors.New("Round requires 1 or 2 arguments")
		}
		n, ok := valueAsFloat(args[0])
		if !ok {
			return Value{}, errors.New("Round requires a number")
		}
		digits := int64(0)
		if len(args) == 2 {
			digits, ok = valueAsInt(args[1])
			if !ok {
				return Value{}, errors.New("Round digits must be an integer")
			}
		}
		scale := math.Pow10(int(digits))
		return FloatValue(math.RoundToEven(n*scale) / scale), nil
	default:
		return Value{}, fmt.Errorf("unsupported Access function %s", call.name)
	}
}

func (e *queryExecutor) evalAggregate(name string, args []sqlExpr, group [][]Value, rel *queryRelation) (Value, error) {
	if name == "COUNT" {
		if len(args) != 1 {
			return Value{}, errors.New("Count requires 1 argument")
		}
		if _, ok := args[0].(sqlStar); ok {
			return IntValue(int64(len(group))), nil
		}
		var count int64
		for _, row := range group {
			v, err := e.eval(args[0], row, rel, nil)
			if err != nil {
				return Value{}, err
			}
			if !v.IsNull() {
				count++
			}
		}
		return IntValue(count), nil
	}
	if len(args) != 1 {
		return Value{}, fmt.Errorf("%s requires 1 argument", name)
	}
	var values []Value
	for _, row := range group {
		v, err := e.eval(args[0], row, rel, nil)
		if err != nil {
			return Value{}, err
		}
		if !v.IsNull() {
			values = append(values, v)
		}
	}
	if len(values) == 0 {
		return NullValue(), nil
	}
	switch name {
	case "MIN":
		best := values[0]
		for _, v := range values[1:] {
			if compareValues(v, best) < 0 {
				best = v
			}
		}
		return best, nil
	case "MAX":
		best := values[0]
		for _, v := range values[1:] {
			if compareValues(v, best) > 0 {
				best = v
			}
		}
		return best, nil
	case "SUM", "AVG":
		var sum float64
		allInt := true
		allCurrency := true
		var currencySum int64
		for _, v := range values {
			n, ok := valueAsFloat(v)
			if !ok {
				return Value{}, fmt.Errorf("%s requires numeric values", name)
			}
			sum += n
			allInt = allInt && v.Kind == ValueInt
			allCurrency = allCurrency && v.Kind == ValueCurrency
			if v.Kind == ValueCurrency {
				currencySum += v.Int
			}
		}
		if name == "AVG" {
			return FloatValue(sum / float64(len(values))), nil
		}
		if allInt {
			return IntValue(int64(sum)), nil
		}
		if allCurrency {
			return CurrencyValue(currencySum), nil
		}
		return FloatValue(sum), nil
	default:
		return Value{}, fmt.Errorf("unsupported aggregate %s", name)
	}
}

func unaryStringFunction(name string, args []Value, fn func(string) Value) (Value, error) {
	if len(args) != 1 {
		return Value{}, fmt.Errorf("%s requires 1 argument", name)
	}
	if args[0].IsNull() {
		return NullValue(), nil
	}
	return fn(args[0].String()), nil
}

func evalBinary(op string, left, right Value) (Value, error) {
	op = strings.ToUpper(op)
	if op == "IS" {
		return BoolValue(left.IsNull() == right.IsNull()), nil
	}
	if op == "AND" || op == "OR" {
		lb, ln := valueTruth(left)
		rb, rn := valueTruth(right)
		if op == "AND" {
			if (!ln && !lb) || (!rn && !rb) {
				return BoolValue(false), nil
			}
			if ln || rn {
				return NullValue(), nil
			}
			return BoolValue(true), nil
		}
		if (!ln && lb) || (!rn && rb) {
			return BoolValue(true), nil
		}
		if ln || rn {
			return NullValue(), nil
		}
		return BoolValue(false), nil
	}
	if left.IsNull() || right.IsNull() {
		return NullValue(), nil
	}
	switch op {
	case "=", "<>", "<", ">", "<=", ">=":
		cmp := compareValues(left, right)
		switch op {
		case "=":
			return BoolValue(cmp == 0), nil
		case "<>":
			return BoolValue(cmp != 0), nil
		case "<":
			return BoolValue(cmp < 0), nil
		case ">":
			return BoolValue(cmp > 0), nil
		case "<=":
			return BoolValue(cmp <= 0), nil
		case ">=":
			return BoolValue(cmp >= 0), nil
		}
	case "LIKE":
		matched, err := accessLike(left.String(), right.String())
		return BoolValue(matched), err
	case "&":
		return StringValue(left.String() + right.String()), nil
	case "+", "-", "*", "/":
		ln, lok := valueAsFloat(left)
		rn, rok := valueAsFloat(right)
		if !lok || !rok {
			if op == "+" && left.Kind == ValueString && right.Kind == ValueString {
				return StringValue(left.Text + right.Text), nil
			}
			return Value{}, fmt.Errorf("operator %s requires numeric operands", op)
		}
		switch op {
		case "+":
			if left.Kind == ValueCurrency && right.Kind == ValueCurrency {
				return CurrencyValue(left.Int + right.Int), nil
			}
			if left.Kind == ValueInt && right.Kind == ValueInt {
				return IntValue(left.Int + right.Int), nil
			}
			return FloatValue(ln + rn), nil
		case "-":
			if left.Kind == ValueCurrency && right.Kind == ValueCurrency {
				return CurrencyValue(left.Int - right.Int), nil
			}
			if left.Kind == ValueInt && right.Kind == ValueInt {
				return IntValue(left.Int - right.Int), nil
			}
			return FloatValue(ln - rn), nil
		case "*":
			if left.Kind == ValueInt && right.Kind == ValueInt {
				return IntValue(left.Int * right.Int), nil
			}
			return FloatValue(ln * rn), nil
		case "/":
			if rn == 0 {
				return Value{}, errors.New("division by zero")
			}
			return FloatValue(ln / rn), nil
		}
	}
	return Value{}, fmt.Errorf("unsupported operator %s", op)
}

func valueTruth(v Value) (bool, bool) {
	switch v.Kind {
	case ValueNull:
		return false, true
	case ValueBool:
		return v.Bool, false
	case ValueInt:
		return v.Int != 0, false
	case ValueFloat:
		return v.Float != 0, false
	case ValueString:
		return v.Text != "" && !strings.EqualFold(v.Text, "false") && v.Text != "0", false
	default:
		return true, false
	}
}

func valueAsInt(v Value) (int64, bool) {
	switch v.Kind {
	case ValueInt:
		return v.Int, true
	case ValueFloat:
		return int64(v.Float), true
	case ValueCurrency:
		return v.Int / 10000, true
	case ValueBool:
		if v.Bool {
			return -1, true
		}
		return 0, true
	case ValueString:
		n, err := strconv.ParseInt(strings.TrimSpace(v.Text), 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func valueAsFloat(v Value) (float64, bool) {
	switch v.Kind {
	case ValueInt:
		return float64(v.Int), true
	case ValueFloat:
		return v.Float, true
	case ValueCurrency:
		return float64(v.Int) / 10000, true
	case ValueBool:
		if v.Bool {
			return -1, true
		}
		return 0, true
	case ValueString:
		n, err := strconv.ParseFloat(strings.TrimSpace(v.Text), 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func compareValues(a, b Value) int {
	if a.IsNull() && b.IsNull() {
		return 0
	}
	if a.IsNull() {
		return -1
	}
	if b.IsNull() {
		return 1
	}
	if an, ok := valueAsFloat(a); ok {
		if bn, ok := valueAsFloat(b); ok {
			if an < bn {
				return -1
			}
			if an > bn {
				return 1
			}
			return 0
		}
	}
	if a.Kind == ValueTime && b.Kind == ValueTime {
		if a.Time.Before(b.Time) {
			return -1
		}
		if a.Time.After(b.Time) {
			return 1
		}
		return 0
	}
	return strings.Compare(strings.ToLower(a.String()), strings.ToLower(b.String()))
}

func containsAggregate(expr sqlExpr) bool {
	switch x := expr.(type) {
	case sqlCall:
		if isAggregateName(strings.ToUpper(x.name)) {
			return true
		}
		for _, arg := range x.args {
			if containsAggregate(arg) {
				return true
			}
		}
	case sqlUnary:
		return containsAggregate(x.expr)
	case sqlBinary:
		return containsAggregate(x.left) || containsAggregate(x.right)
	case sqlIn:
		if containsAggregate(x.expr) {
			return true
		}
		for _, item := range x.list {
			if containsAggregate(item) {
				return true
			}
		}
	case sqlBetween:
		return containsAggregate(x.expr) || containsAggregate(x.low) || containsAggregate(x.high)
	}
	return false
}

func statementHasAggregate(stmt *sqlSelect) bool {
	if stmt == nil {
		return false
	}
	if len(stmt.group) > 0 || containsAggregate(stmt.having) {
		return true
	}
	for _, item := range stmt.items {
		if containsAggregate(item.expr) {
			return true
		}
	}
	return false
}

func resolveOrderAliases(order []sqlOrder, items []sqlSelectItem) []sqlOrder {
	out := append([]sqlOrder(nil), order...)
	for i := range out {
		switch expr := out[i].expr.(type) {
		case sqlIdent:
			if len(expr.parts) != 1 {
				continue
			}
			for _, item := range items {
				if item.alias != "" && strings.EqualFold(item.alias, expr.parts[0]) {
					out[i].expr = item.expr
					break
				}
			}
		case sqlLiteral:
			if expr.value.Kind == ValueInt && expr.value.Int >= 1 && int(expr.value.Int) <= len(items) {
				out[i].expr = items[expr.value.Int-1].expr
			}
		}
	}
	return out
}

func isAggregateName(name string) bool {
	switch name {
	case "COUNT", "SUM", "AVG", "MIN", "MAX":
		return true
	default:
		return false
	}
}

func expressionName(expr sqlExpr) string {
	switch x := expr.(type) {
	case sqlIdent:
		return x.parts[len(x.parts)-1]
	case sqlCall:
		return x.name
	case sqlLiteral:
		return "Expr"
	default:
		return "Expr"
	}
}

func rowKey(row []Value) string {
	var b strings.Builder
	for _, value := range row {
		b.WriteByte(byte(value.Kind))
		s := value.String()
		b.WriteString(strconv.Itoa(len(s)))
		b.WriteByte(':')
		b.WriteString(s)
		b.WriteByte('|')
	}
	return b.String()
}

func distinctRows(rows [][]Value) [][]Value {
	seen := make(map[string]struct{}, len(rows))
	out := rows[:0]
	for _, row := range rows {
		key := rowKey(row)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, row)
	}
	return out
}

func simpleScanLimit(stmt *sqlSelect) int {
	if stmt == nil {
		return 0
	}
	if _, ok := stmt.source.(sqlTableSource); !ok {
		return 0
	}
	if stmt.where != nil || len(stmt.group) != 0 ||
		stmt.having != nil || len(stmt.order) != 0 || stmt.distinct || stmt.distinctRow || stmt.union != nil {
		return 0
	}
	limit := 0
	if stmt.limit >= 0 {
		limit = stmt.offset + stmt.limit
	}
	if stmt.top >= 0 && !stmt.topPercent && (limit == 0 || stmt.top < limit) {
		limit = stmt.top
	}
	return limit
}

// requiredColumnsFor returns nil when the source must be scanned with every
// column (SELECT * / source.*). Otherwise it returns the referenced names.
func requiredColumnsFor(stmt *sqlSelect, ref sqlTableRef) []string {
	source := ref.alias
	if source == "" {
		source = ref.name
	}
	required := make(map[string]string)
	all := false
	var walk func(sqlExpr)
	walk = func(expr sqlExpr) {
		switch x := expr.(type) {
		case sqlStar:
			if x.qualifier == "" || strings.EqualFold(x.qualifier, source) || strings.EqualFold(x.qualifier, ref.name) {
				all = true
			}
		case sqlIdent:
			if len(x.parts) == 1 {
				required[strings.ToLower(x.parts[0])] = x.parts[0]
			} else {
				qualifier := x.parts[len(x.parts)-2]
				if strings.EqualFold(qualifier, source) || strings.EqualFold(qualifier, ref.name) {
					name := x.parts[len(x.parts)-1]
					required[strings.ToLower(name)] = name
				}
			}
		case sqlUnary:
			walk(x.expr)
		case sqlBinary:
			walk(x.left)
			walk(x.right)
		case sqlCall:
			for _, arg := range x.args {
				walk(arg)
			}
		case sqlIn:
			walk(x.expr)
			for _, item := range x.list {
				walk(item)
			}
		case sqlBetween:
			walk(x.expr)
			walk(x.low)
			walk(x.high)
		}
	}
	for _, item := range stmt.items {
		walk(item.expr)
	}
	walk(stmt.where)
	for _, expr := range stmt.group {
		walk(expr)
	}
	walk(stmt.having)
	for _, order := range stmt.order {
		walk(order.expr)
	}
	var walkJoins func(sqlSource)
	walkJoins = func(source sqlSource) {
		if join, ok := source.(sqlJoinSource); ok {
			walkJoins(join.left)
			walkJoins(join.right)
			walk(join.on)
		}
	}
	walkJoins(stmt.source)
	if all {
		return nil
	}
	result := make([]string, 0, len(required))
	for _, name := range required {
		result = append(result, name)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

func filterRequestedColumns(requested []string, schema *TableSchema) []string {
	if requested == nil {
		return nil
	}
	wanted := make(map[string]struct{}, len(requested))
	for _, name := range requested {
		wanted[strings.ToLower(name)] = struct{}{}
	}
	result := make([]string, 0, len(requested))
	for _, col := range schema.Columns {
		if _, ok := wanted[strings.ToLower(col.Name)]; ok {
			result = append(result, col.Name)
		}
	}
	// A constant-only query still needs a physical row source to determine
	// cardinality; bind the narrowest available column.
	if len(result) == 0 && len(schema.Columns) > 0 {
		result = append(result, schema.Columns[0].Name)
	}
	return result
}

func cloneValues(values []Value) []Value {
	out := append([]Value(nil), values...)
	for i := range out {
		if out[i].Kind == ValueBytes {
			out[i].Bytes = append([]byte(nil), out[i].Bytes...)
		}
	}
	return out
}

func accessLike(value, pattern string) (bool, error) {
	key := pattern
	var re *regexp.Regexp
	if cached, ok := accessLikeCache.Load(key); ok {
		re = cached.(*regexp.Regexp)
	} else {
		var b strings.Builder
		b.WriteString("(?is)^")
		inClass := false
		for _, r := range pattern {
			switch r {
			case '*', '%':
				if inClass {
					b.WriteRune(r)
				} else {
					b.WriteString(".*")
				}
			case '?', '_':
				if inClass {
					b.WriteRune(r)
				} else {
					b.WriteByte('.')
				}
			case '[':
				inClass = true
				b.WriteRune(r)
			case ']':
				inClass = false
				b.WriteRune(r)
			case '!':
				if inClass {
					b.WriteByte('^')
				} else {
					b.WriteString(`\!`)
				}
			default:
				if inClass {
					b.WriteRune(r)
				} else {
					b.WriteString(regexp.QuoteMeta(string(r)))
				}
			}
		}
		b.WriteByte('$')
		var err error
		re, err = regexp.Compile(b.String())
		if err != nil {
			return false, fmt.Errorf("invalid LIKE pattern %q: %w", pattern, err)
		}
		accessLikeCache.Store(key, re)
	}
	return re.MatchString(value), nil
}
