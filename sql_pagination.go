package mdbgo

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
)

const (
	defaultPageSize = 100
	maxPageSize     = 1000
	cursorVersion   = 2
)

// PageRequest requests one page. After and Before are opaque cursors returned
// by an earlier call. They are mutually exclusive.
type PageRequest struct {
	Size   int
	After  string
	Before string
}

// PageResult is a stable page of typed query values.
type PageResult struct {
	Columns    []ResultColumn `json:"columns"`
	Rows       [][]Value      `json:"rows"`
	NextCursor string         `json:"nextCursor,omitempty"`
	PrevCursor string         `json:"prevCursor,omitempty"`
	HasMore    bool           `json:"hasMore"`
}

type pageCursor struct {
	Version    int    `json:"v"`
	QueryHash  string `json:"q"`
	DBIdentity string `json:"d"`
	AnchorHash string `json:"a"`
	Occurrence int    `json:"n"`
}

// QueryPageContext executes a stably ordered query and returns one cursor page.
//
// The v1 implementation validates stateless cursors and gives correct stable
// pages. It intentionally keeps the cursor wire format private so an indexed
// keyset implementation can replace the internal offset without changing API.
func (db *DB) QueryPageContext(
	ctx context.Context,
	sqlText string,
	params map[string]any,
	request PageRequest,
) (*PageResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if request.After != "" && request.Before != "" {
		return nil, errors.New("After and Before are mutually exclusive")
	}
	stmt, err := parseAccessSQL(sqlText)
	if err != nil {
		return nil, err
	}
	session, release, err := db.acquireQuerySession(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	if len(stmt.order) == 0 {
		table, ok := stmt.source.(sqlTableSource)
		if !ok || len(stmt.group) != 0 || stmt.distinct || stmt.distinctRow || stmt.union != nil {
			return nil, errors.New("cursor pagination requires an explicit ORDER BY for joined, grouped, distinct, or union queries")
		}
		schema, err := session.Schema(table.ref.name)
		if err != nil {
			return nil, fmt.Errorf("infer cursor ordering: %w", err)
		}
		if len(schema.Columns) == 0 {
			return nil, errors.New("cursor pagination cannot infer an ordering column")
		}
		stmt.order = []sqlOrder{{expr: sqlIdent{parts: []string{schema.Columns[0].Name}}}}
	}
	size := request.Size
	if size == 0 {
		size = defaultPageSize
	}
	if size < 1 || size > maxPageSize {
		return nil, fmt.Errorf("page size must be between 1 and %d", maxPageSize)
	}
	hash, err := queryFingerprint(sqlText, params)
	if err != nil {
		return nil, err
	}
	dbIdentity := db.fileIdentity()
	start := 0
	beforeEnd := -1
	cursorText := request.After
	if cursorText == "" {
		cursorText = request.Before
	}
	if cursorText != "" {
		cursor, err := decodePageCursor(cursorText)
		if err != nil {
			return nil, err
		}
		if cursor.Version != cursorVersion || cursor.QueryHash != hash {
			return nil, errors.New("invalid cursor: SQL or parameters changed")
		}
		if cursor.DBIdentity != dbIdentity {
			return nil, errors.New("stale cursor: database file changed")
		}
		if cursor.Occurrence < 1 || cursor.AnchorHash == "" {
			return nil, errors.New("invalid cursor anchor")
		}
		// The anchor is resolved after execution. The cursor contains no page
		// number or offset, so inserting earlier rows cannot silently target an
		// unrelated position.
		start = -1
	}
	rows, err := session.queryStatement(ctx, stmt, params)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all := rows.All()
	if cursorText != "" {
		cursor, _ := decodePageCursor(cursorText)
		anchorIndex := findCursorAnchor(all, cursor.AnchorHash, cursor.Occurrence)
		if anchorIndex < 0 {
			return nil, errors.New("stale cursor: anchor row is no longer present")
		}
		if request.Before != "" {
			beforeEnd = anchorIndex
			start = beforeEnd - size
			if start < 0 {
				start = 0
			}
		} else {
			start = anchorIndex + 1
		}
	}
	if start > len(all) {
		start = len(all)
	}
	end := start + size
	if beforeEnd >= 0 {
		end = beforeEnd
	}
	if end > len(all) {
		end = len(all)
	}
	result := &PageResult{
		Columns: rows.Columns(),
		Rows:    cloneRows(all[start:end]),
		HasMore: end < len(all),
	}
	if end < len(all) {
		anchorHash, occurrence := cursorAnchorAt(all, end-1)
		result.NextCursor, err = encodePageCursor(pageCursor{
			Version: cursorVersion, QueryHash: hash, DBIdentity: dbIdentity,
			AnchorHash: anchorHash, Occurrence: occurrence,
		})
		if err != nil {
			return nil, err
		}
	}
	if start > 0 {
		anchorHash, occurrence := cursorAnchorAt(all, start)
		result.PrevCursor, err = encodePageCursor(pageCursor{
			Version: cursorVersion, QueryHash: hash, DBIdentity: dbIdentity,
			AnchorHash: anchorHash, Occurrence: occurrence,
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func encodePageCursor(cursor pageCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodePageCursor(text string) (pageCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return pageCursor{}, errors.New("invalid cursor encoding")
	}
	var cursor pageCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return pageCursor{}, errors.New("invalid cursor payload")
	}
	return cursor, nil
}

func cursorAnchorAt(rows [][]Value, index int) (string, int) {
	if index < 0 || index >= len(rows) {
		return "", 0
	}
	hash := rowFingerprint(rows[index])
	occurrence := 0
	for i := 0; i <= index; i++ {
		if rowFingerprint(rows[i]) == hash {
			occurrence++
		}
	}
	return hash, occurrence
}

func findCursorAnchor(rows [][]Value, hash string, occurrence int) int {
	seen := 0
	for i, row := range rows {
		if rowFingerprint(row) != hash {
			continue
		}
		seen++
		if seen == occurrence {
			return i
		}
	}
	return -1
}

func rowFingerprint(row []Value) string {
	sum := sha256.Sum256([]byte(rowKey(row)))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func queryFingerprint(sqlText string, params map[string]any) (string, error) {
	normalized := struct {
		SQL    string         `json:"sql"`
		Params map[string]any `json:"params"`
	}{
		SQL:    strings.TrimSpace(sqlText),
		Params: params,
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("encode query fingerprint: %w", err)
	}
	sum := sha256.Sum256(data)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func (db *DB) fileIdentity() string {
	if db == nil || db.path == "" {
		return ""
	}
	info, err := os.Stat(db.path)
	if err != nil {
		return db.path
	}
	value := fmt.Sprintf("%s\x00%d\x00%d", db.path, info.Size(), info.ModTime().UnixNano())
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Pager is an on-disk stable snapshot used for random page access.
type Pager struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	columns  []ResultColumn
	offsets  []int64
	pageSize int
	closed   bool
}

// PreparePagerContext executes the query once and writes each result row to an
// indexed temporary spool. Page numbers are one-based.
func (db *DB) PreparePagerContext(
	ctx context.Context,
	sqlText string,
	params map[string]any,
	pageSize int,
) (*Pager, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if pageSize < 1 || pageSize > maxPageSize {
		return nil, fmt.Errorf("page size must be between 1 and %d", maxPageSize)
	}
	rows, err := db.QueryContext(ctx, sqlText, params)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	file, err := os.CreateTemp("", "mdbgo-query-*.spool")
	if err != nil {
		return nil, fmt.Errorf("create query spool: %w", err)
	}
	pager := &Pager{
		file: file, path: file.Name(), columns: rows.Columns(), pageSize: pageSize,
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = pager.Close()
		}
	}()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		offset, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(rows.Values())
		if err != nil {
			return nil, err
		}
		if len(data) > int(^uint32(0)) {
			return nil, errors.New("query row is too large for spool")
		}
		if err := binary.Write(file, binary.LittleEndian, uint32(len(data))); err != nil {
			return nil, err
		}
		if _, err := file.Write(data); err != nil {
			return nil, err
		}
		pager.offsets = append(pager.offsets, offset)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	runtime.SetFinalizer(pager, func(p *Pager) {
		_ = p.Close()
	})
	cleanup = false
	return pager, nil
}

// Page returns a one-based page from the stable snapshot.
func (p *Pager) Page(ctx context.Context, pageNumber int) (*PageResult, error) {
	if p == nil {
		return nil, errors.New("nil pager")
	}
	if pageNumber < 1 {
		return nil, errors.New("page number must be >= 1")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.file == nil {
		return nil, errors.New("pager is closed")
	}
	start := (pageNumber - 1) * p.pageSize
	if start > len(p.offsets) {
		start = len(p.offsets)
	}
	end := start + p.pageSize
	if end > len(p.offsets) {
		end = len(p.offsets)
	}
	result := &PageResult{
		Columns: append([]ResultColumn(nil), p.columns...),
		Rows:    make([][]Value, 0, end-start),
		HasMore: end < len(p.offsets),
	}
	for i := start; i < end; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := p.file.Seek(p.offsets[i], io.SeekStart); err != nil {
			return nil, err
		}
		var size uint32
		if err := binary.Read(p.file, binary.LittleEndian, &size); err != nil {
			return nil, err
		}
		data := make([]byte, int(size))
		if _, err := io.ReadFull(p.file, data); err != nil {
			return nil, err
		}
		var row []Value
		if err := json.Unmarshal(data, &row); err != nil {
			return nil, err
		}
		result.Rows = append(result.Rows, row)
	}
	return result, nil
}

func (p *Pager) PageCount() int {
	if p == nil || p.pageSize <= 0 {
		return 0
	}
	return (len(p.offsets) + p.pageSize - 1) / p.pageSize
}

func (p *Pager) RowCount() int {
	if p == nil {
		return 0
	}
	return len(p.offsets)
}

func (p *Pager) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	runtime.SetFinalizer(p, nil)
	var closeErr, removeErr error
	if p.file != nil {
		closeErr = p.file.Close()
		p.file = nil
	}
	if p.path != "" {
		removeErr = os.Remove(p.path)
		p.path = ""
	}
	if closeErr != nil {
		return closeErr
	}
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	return nil
}

func cloneRows(rows [][]Value) [][]Value {
	out := make([][]Value, len(rows))
	for i := range rows {
		out[i] = cloneValues(rows[i])
	}
	return out
}
