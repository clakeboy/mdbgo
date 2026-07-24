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
*/
import "C"

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"
)

const cErrBufSize = 2048

// DatabaseFormat 描述当前打开的 Access 数据库文件格式。
type DatabaseFormat struct {
	// Name 是面向调用方展示的格式名称，例如 "Access 2000"。
	Name string
	// Engine 是底层数据库引擎系列，例如 "Jet 4" 或 "ACE"。
	Engine string
	// Version 是 MDB/ACCDB 文件头中的原始格式版本号。
	Version int
	// PageSize 是数据库页面大小，单位为字节。
	PageSize int
	// ObjectStorage 是应用对象使用的系统存储表；没有时为空。
	ObjectStorage string
}

// String 返回面向展示的格式名称。
func (format DatabaseFormat) String() string {
	return format.Name
}

// OpenOptions controls resources owned by a DB.
type OpenOptions struct {
	// MaxConcurrentQueries is the maximum number of Query, QueryContext,
	// QueryViewContext, and QueryPageContext calls that may execute at once.
	//
	// Zero selects a runtime-derived default. Each active query uses an
	// independent mdbtools handle because a single MdbHandle is not safe for
	// concurrent scans.
	MaxConcurrentQueries int
}

// DB 表示一个 MDB 数据库连接句柄。
//
// Query、QueryContext、QueryViewContext、QueryPageContext 和
// PreparePagerContext 可由多个 goroutine 并发调用。每个正在执行的查询会使用
// 独立的 mdbtools 句柄。其它直接读取 API 仍使用主句柄，不应彼此并发调用。
type DB struct {
	ptr  *C.mdbgo_db_t
	path string

	// Format 是当前打开文件的格式信息，在 Open/OpenWithOptions 成功后保持不变。
	Format DatabaseFormat

	stateMu   sync.Mutex
	closed    bool
	queryPool *queryHandlePool

	metaMu      sync.Mutex
	tableCache  []string
	viewCache   []string
	tableLookup map[string]string
	viewLookup  map[string]string
	schemaCache map[string]*TableSchema
}

// Open 打开一个 MDB 文件并返回 DB。
func Open(path string) (*DB, error) {
	return OpenWithOptions(path, OpenOptions{})
}

// OpenWithOptions 打开一个 MDB 文件，并允许配置并发查询句柄数量。
func OpenWithOptions(path string, options OpenOptions) (*DB, error) {
	if path == "" {
		return nil, errors.New("path is empty")
	}
	if options.MaxConcurrentQueries < 0 {
		return nil, errors.New("MaxConcurrentQueries must be >= 0")
	}
	if options.MaxConcurrentQueries > maxConfiguredConcurrentQueries {
		return nil, fmt.Errorf("MaxConcurrentQueries must be <= %d", maxConfiguredConcurrentQueries)
	}

	db, err := openDBHandle(path)
	if err != nil {
		return nil, err
	}
	format, err := db.detectDatabaseFormat()
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("detect database format: %w", err)
	}
	db.Format = format
	maxQueries := options.MaxConcurrentQueries
	if maxQueries == 0 {
		maxQueries = defaultMaxConcurrentQueries()
	}
	db.queryPool = newQueryHandlePool(db.path, maxQueries)
	// 设置终结器兜底，避免调用方忘记 Close 导致 C 侧句柄泄漏。
	runtime.SetFinalizer(db, func(d *DB) {
		_ = d.Close()
	})
	return db, nil
}

func openDBHandle(path string) (*DB, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	errBuf := make([]byte, cErrBufSize)
	var dbPtr *C.mdbgo_db_t
	rc := C.mdbgo_open(cPath, &dbPtr, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}

	identityPath := path
	if absolute, err := filepath.Abs(path); err == nil {
		identityPath = absolute
	}
	return &DB{ptr: dbPtr, path: identityPath}, nil
}

func (db *DB) detectDatabaseFormat() (DatabaseFormat, error) {
	if db == nil || db.ptr == nil {
		return DatabaseFormat{}, errors.New("db is closed")
	}

	errBuf := make([]byte, cErrBufSize)
	var version C.int
	var pageSize C.size_t
	rc := C.mdbgo_get_file_format(
		db.ptr,
		&version,
		&pageSize,
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.size_t(len(errBuf)),
	)
	if rc != 0 {
		return DatabaseFormat{}, errors.New(cStringFromBuf(errBuf))
	}

	format := DatabaseFormat{
		Version:  int(version),
		PageSize: int(pageSize),
	}
	storageKind, err := db.accessObjectStorageKind()
	if err != nil {
		return DatabaseFormat{}, err
	}
	switch storageKind {
	case accessObjectStorageObjects:
		format.ObjectStorage = "MSysAccessObjects"
	case accessObjectStorageTree:
		format.ObjectStorage = "MSysAccessStorage"
	}

	switch format.Version {
	case 0:
		format.Name = "Access 97"
		format.Engine = "Jet 3"
	case 1:
		format.Name = "Access 2000-2003"
		format.Engine = "Jet 4"
		switch storageKind {
		case accessObjectStorageObjects:
			format.Name = "Access 2000"
		case accessObjectStorageTree:
			format.Name = "Access 2003"
		}
	case 2:
		format.Name = "Access 2007"
		format.Engine = "ACE"
	case 3:
		format.Name = "Access 2010"
		format.Engine = "ACE"
	case 4:
		format.Name = "Access 2013"
		format.Engine = "ACE"
	case 5:
		format.Name = "Access 2016"
		format.Engine = "ACE"
	case 6:
		format.Name = "Access 2019"
		format.Engine = "ACE"
	default:
		format.Name = fmt.Sprintf("Access format 0x%02x", format.Version)
		format.Engine = "Unknown"
	}
	return format, nil
}

// Close 释放底层 C 句柄，支持重复调用。
func (db *DB) Close() error {
	if db == nil {
		return nil
	}

	db.stateMu.Lock()
	if db.closed {
		db.stateMu.Unlock()
		return nil
	}
	db.closed = true
	ptr := db.ptr
	db.ptr = nil
	pool := db.queryPool
	db.queryPool = nil
	db.stateMu.Unlock()

	runtime.SetFinalizer(db, nil)
	if pool != nil {
		pool.close()
	}
	if ptr != nil {
		C.mdbgo_close(ptr)
	}
	db.metaMu.Lock()
	db.tableCache = nil
	db.viewCache = nil
	db.tableLookup = nil
	db.viewLookup = nil
	db.schemaCache = nil
	db.metaMu.Unlock()
	return nil
}

func (db *DB) closeQueryHandle() {
	if db == nil {
		return
	}
	db.stateMu.Lock()
	ptr := db.ptr
	db.ptr = nil
	db.closed = true
	db.stateMu.Unlock()
	if ptr != nil {
		C.mdbgo_close(ptr)
	}
}

// cStringFromBuf 从 C 写入的错误缓冲区提取到第一个 NUL 字节为止的字符串。
func cStringFromBuf(buf []byte) string {
	for i, b := range buf {
		if b == 0 {
			if i == 0 {
				return "unknown error"
			}
			return string(buf[:i])
		}
	}
	if len(buf) == 0 {
		return "unknown error"
	}
	return string(buf)
}
