package mdbgo

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	purego "github.com/clakeboy/mdbgo/purego"
)

// DatabaseFormat 描述当前打开的 Access 数据库文件格式。
type DatabaseFormat struct {
	Name           string
	Engine         string
	Version        int
	PageSize       int
	ObjectStorage  string
}

func (format DatabaseFormat) String() string {
	return format.Name
}

// DB 表示一个 MDB 数据库连接句柄。
type DB struct {
	puregoDB *purego.MDB
	handle   *purego.MdbHandle
	path     string

	Format DatabaseFormat

	stateMu sync.Mutex
	closed  bool

	metaMu      sync.Mutex
	tableCache  []string
	viewCache   []string
	tableLookup map[string]string
	viewLookup  map[string]string
	schemaCache map[string]*TableSchema
}

// Open 打开一个 MDB 文件并返回 DB。
func Open(path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("path is empty")
	}

	pureDB, err := purego.OpenMDB(path)
	if err != nil {
		return nil, err
	}

	identityPath := path
	if absolute, err := filepath.Abs(path); err == nil {
		identityPath = absolute
	}

	db := &DB{
		puregoDB: pureDB,
		handle:   pureDB.GetHandle(),
		path:     identityPath,
	}

	format, err := db.detectDatabaseFormat()
	if err != nil {
		pureDB.Close()
		return nil, fmt.Errorf("detect database format: %w", err)
	}
	db.Format = format

	runtime.SetFinalizer(db, func(d *DB) {
		_ = d.Close()
	})
	return db, nil
}

// OpenPureGo 保留兼容别名，等同 Open。
func OpenPureGo(path string) (*DB, error) {
	return Open(path)
}

func (db *DB) detectDatabaseFormat() (DatabaseFormat, error) {
	if db == nil || db.handle == nil {
		return DatabaseFormat{}, errors.New("db is closed")
	}

	jetVer, pgSize := db.handle.GetFileFormat()
	format := DatabaseFormat{
		Version:  int(jetVer),
		PageSize: pgSize,
	}
	storageKind := db.detectStorageKind()
	switch storageKind {
	case purego.AccessStorageObjects:
		format.ObjectStorage = "MSysAccessObjects"
	case purego.AccessStorageTree:
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
		case purego.AccessStorageObjects:
			format.Name = "Access 2000"
		case purego.AccessStorageTree:
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

func (db *DB) detectStorageKind() int {
	if db.handle == nil {
		return purego.AccessStorageNone
	}
	catalog := db.handle.ReadCatalog(purego.MDBAny)
	if catalog == nil {
		return purego.AccessStorageNone
	}
	for _, entry := range catalog {
		if entry == nil {
			continue
		}
		if strings.EqualFold(entry.ObjectName, "MSysAccessObjects") {
			return purego.AccessStorageObjects
		}
		if strings.EqualFold(entry.ObjectName, "MSysAccessStorage") {
			return purego.AccessStorageTree
		}
	}
	return purego.AccessStorageNone
}

// Close 释放底层句柄，支持重复调用。
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
	pureDB := db.puregoDB
	db.puregoDB = nil
	db.handle = nil
	db.stateMu.Unlock()

	runtime.SetFinalizer(db, nil)
	if pureDB != nil {
		pureDB.Close()
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
