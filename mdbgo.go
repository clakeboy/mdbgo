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
#cgo CFLAGS: -DHAVE_REALLOCF=1
#include <stdlib.h>
#include "bridge.h"
*/
import "C"

import (
	"errors"
	"runtime"
	"unsafe"
)

const cErrBufSize = 2048

// DB 表示一个 MDB 数据库连接句柄。
// 当前实现为“单句柄串行访问”模型，不保证并发安全。
type DB struct {
	ptr *C.mdbgo_db_t
}

// Open 打开一个 MDB 文件并返回 DB。
func Open(path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("path is empty")
	}

	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	errBuf := make([]byte, cErrBufSize)
	var dbPtr *C.mdbgo_db_t
	rc := C.mdbgo_open(cPath, &dbPtr, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}

	db := &DB{ptr: dbPtr}
	// 设置终结器兜底，避免调用方忘记 Close 导致 C 侧句柄泄漏。
	runtime.SetFinalizer(db, func(d *DB) {
		_ = d.Close()
	})
	return db, nil
}

// Close 释放底层 C 句柄，支持重复调用。
func (db *DB) Close() error {
	if db == nil || db.ptr == nil {
		return nil
	}

	C.mdbgo_close(db.ptr)
	db.ptr = nil
	return nil
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
