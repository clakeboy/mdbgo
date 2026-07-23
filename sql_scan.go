package mdbgo

/*
#cgo CFLAGS: -I${SRCDIR}/internal/bundled
#cgo CFLAGS: -I${SRCDIR}/internal/bundled/include
#cgo CFLAGS: -DTLS=__thread
#cgo CFLAGS: -DICONV_CONST=const
#cgo CFLAGS: -DHAVE_STRTOK_R=1
#cgo CFLAGS: -DHAVE_SETLOCALE=1
#cgo CFLAGS: -DHAVE_SYS_STAT_H=1
#cgo CFLAGS: -DHAVE_SYS_TYPES_H=1
#cgo darwin CFLAGS: -DHAVE_REALLOCF=1
#include <stdlib.h>
#include "bridge.h"

typedef struct mdbgo_scan mdbgo_scan_t;
int mdbgo_scan_open(
    mdbgo_db_t *db,
    const char *table_name,
    const char *const *requested_columns,
    size_t requested_count,
    mdbgo_scan_t **out_scan,
    char *err,
    size_t err_len);
int mdbgo_scan_next(
    mdbgo_scan_t *scan,
    size_t max_rows,
    mdbgo_table_data_t *out,
    char *err,
    size_t err_len);
void mdbgo_scan_close(mdbgo_scan_t *scan);
*/
import "C"

import (
	"context"
	"errors"
	"unsafe"
)

const defaultScanBatchRows = 1024

// readTableBatched crosses the CGO boundary once per batch instead of once per
// row. requestedColumns may be nil to scan every column. maxRows <= 0 means no
// early row limit.
func (db *DB) readTableBatched(
	ctx context.Context,
	tableName string,
	requestedColumns []string,
	maxRows int,
) (*TableData, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cName := C.CString(tableName)
	defer C.free(unsafe.Pointer(cName))

	cColumns := make([]*C.char, len(requestedColumns))
	for i := range requestedColumns {
		cColumns[i] = C.CString(requestedColumns[i])
		defer C.free(unsafe.Pointer(cColumns[i]))
	}
	var columnsPtr **C.char
	if len(cColumns) > 0 {
		columnsPtr = (**C.char)(unsafe.Pointer(&cColumns[0]))
	}
	errBuf := make([]byte, cErrBufSize)
	var scan *C.mdbgo_scan_t
	rc := C.mdbgo_scan_open(
		db.ptr,
		cName,
		columnsPtr,
		C.size_t(len(cColumns)),
		&scan,
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.size_t(len(errBuf)),
	)
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}
	defer C.mdbgo_scan_close(scan)

	result := &TableData{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batchSize := defaultScanBatchRows
		if maxRows > 0 {
			remaining := maxRows - len(result.Rows)
			if remaining <= 0 {
				break
			}
			if remaining < batchSize {
				batchSize = remaining
			}
		}
		var raw C.mdbgo_table_data_t
		rc = C.mdbgo_scan_next(
			scan,
			C.size_t(batchSize),
			&raw,
			(*C.char)(unsafe.Pointer(&errBuf[0])),
			C.size_t(len(errBuf)),
		)
		if rc != 0 {
			return nil, errors.New(cStringFromBuf(errBuf))
		}
		batch, err := rawTableDataToGo(&raw)
		C.mdbgo_free_table_data(&raw)
		if err != nil {
			return nil, err
		}
		if result.Columns == nil {
			result.Columns = batch.Columns
		}
		result.Rows = append(result.Rows, batch.Rows...)
		result.Nulls = append(result.Nulls, batch.Nulls...)
		if len(batch.Rows) < batchSize {
			break
		}
	}
	return result, nil
}
