package purego

// MDB 文件格式版本
const (
	MDBVerJet3       = 0
	MDBVerJet4       = 0x01
	MDBVerAccdb2007  = 0x02
	MDBVerAccdb2010  = 0x03
	MDBVerAccdb2013  = 0x04
	MDBVerAccdb2016  = 0x05
	MDBVerAccdb2019  = 0x06
)

// 页面类型
const (
	MDBPageDB    = 0
	MDBPageData  = 1
	MDBPageTable = 2
	MDBPageIndex = 3
	MDBPageLeaf  = 4
	MDBPageMap   = 5
)

// 对象类型
const (
	MDBForm             = 0
	MDBTable            = 1
	MDBMacro            = 2
	MDBSystemTable      = 3
	MDBReport           = 4
	MDBQuery            = 5
	MDBLinkedTable      = 6
	MDBModule           = 7
	MDBRelationship     = 8
	MDBUnknown09        = 9
	MDBUnknown0A        = 10
	MDBDatabaseProperty = 11
	MDBAny              = -1
)

// 列数据类型
const (
	MDBBool     = 0x01
	MDBByte     = 0x02
	MDBInt      = 0x03
	MDBLongInt  = 0x04
	MDBMoney    = 0x05
	MDBFloat    = 0x06
	MDBDouble   = 0x07
	MDBDateTime = 0x08
	MDBBinary   = 0x09
	MDBText     = 0x0a
	MDBOle      = 0x0b
	MDBMemo     = 0x0c
	MDBRepId    = 0x0f
	MDBNumeric  = 0x10
	MDBComplex  = 0x12
)

// SARG 操作符
const (
	MDBOr     = 1
	MDBAnd    = 2
	MDBNot    = 3
	MDBEqual  = 4
	MDBGT     = 5
	MDBLT     = 6
	MDBGTEQ   = 7
	MDBLTEQ   = 8
	MDBLike   = 9
	MDBIsNull = 10
	MDBNotNull = 11
	MDBILike  = 12
	MDBNEQ    = 13
)

// 扫描策略
type MdbStrategy int

const (
	MDBTableScan MdbStrategy = iota
	MDBLeafScan
	MDBIndexScan
)

// 文件打开标志
type MdbFileFlags int

const (
	MDBNoFlags  MdbFileFlags = 0x00
	MDBWritable MdbFileFlags = 0x01
)

// 调试标志
const (
	MDBDebugLike   = 0x0001
	MDBDebugWrite  = 0x0002
	MDBDebugUsage  = 0x0004
	MDBDebugOle    = 0x0008
	MDBDebugRow    = 0x0010
	MDBDebugProps  = 0x0020
	MDBUseIndex    = 0x0040
	MDBNoMemo      = 0x0080
)

// UUID 格式
type MdbUuidFormat int

const (
	MDBBraces4228    MdbUuidFormat = iota // "{XXXX-XX-XX-XXXXXXXX}" format
	MDBNoBraces42226                     // "XXXX-XX-XX-XX-XXXXXX" format
)

// 排序方向
const (
	MDBAsc  = 0
	MDBDesc = 1
)

// 索引标志
const (
	MDBIdxUnique     = 0x01
	MDBIdxIgnoreNull = 0x02
	MDBIdxRequired   = 0x08
)

// 常量
const (
	MDBPageSize     = 4096
	MDBMaxObjName   = 256
	MDBMaxCols      = 256
	MDBMaxIdxCols   = 10
	MDBCatalogPG    = 18
	MDBMemoOverhead = 12
	MDBBindSize     = 16384
	MDBMaxIndexDepth = 10
)

// MdbAny 联合类型
type MdbAny struct {
	I int32
	D float64
	S [256]byte
}

// MdbFile 文件句柄
type MdbFile struct {
	Stream    *MdbFileHandle
	Reader    *MdbFileReader
	Writable  bool
	JetVersion uint32
	DBKey     uint32
	DBPasswd  [14]byte
	Stats     *MdbStatistics
	FreeMap   []byte
	MapSize   int
	Refs      int
	CodePage  uint16
	LangID    uint16
}

// MdbFileHandle 文件操作句柄
type MdbFileHandle struct {
	File     interface{ Read([]byte) (int, error) }
	Offset   int64
	Size     int64
}

// MdbStatistics I/O 统计
type MdbStatistics struct {
	Collect  bool
	PgReads  uint64
}

// MdbFormatConstants 格式常量
type MdbFormatConstants struct {
	PgSize             int
	RowCountOffset     uint16
	TabNumRowsOffset   uint16
	TabNumColsOffset   uint16
	TabNumIdxsOffset   uint16
	TabNumRidxsOffset  uint16
	TabUsageMapOffset  uint16
	TabFirstDpgOffset  uint16
	TabColsStartOffset uint16
	TabRidxEntrySize   uint16
	ColFlagsOffset     uint16
	ColSizeOffset      uint16
	ColNumOffset       uint16
	TabColEntrySize    uint16
	TabFreeMapOffset   uint16
	TabColOffsetVar    uint16
	TabColOffsetFixed  uint16
	TabRowColNumOffset uint16
	ColScaleOffset     uint16
	ColPrecOffset      uint16
}

// MdbHandle 数据库句柄
type MdbHandle struct {
	F             *MdbFile
	CurPg         uint32
	RowNum        uint16
	CurPos        int
	PgBuf         [MDBPageSize]byte
	AltPgBuf      [MDBPageSize]byte
	Fmt           *MdbFormatConstants
	BindSize      int
	DateFmt       string
	ShortDateFmt  string
	RepidFmt      MdbUuidFormat
	BooleanFalse  string
	BooleanTrue   string
	NumCatalog    uint
	Catalog       []*MdbCatalogEntry
	DefaultBackend *MdbBackend
	BackendName   string
	Stats         *MdbStatistics
	Backends      map[string]*MdbBackend
	Locale        interface{}
}

// MdbCatalogEntry 目录条目
type MdbCatalogEntry struct {
	Mdb        *MdbHandle
	ObjectName string
	ObjectType int
	TablePg    uint32
	Props      []*MdbProperties
	Flags      int
}

// MdbProperties 属性
type MdbProperties struct {
	Name string
	Hash map[string]interface{}
}

// MdbColumn 列定义
type MdbColumn struct {
	Table          *MdbTableDef
	Name           string
	ColType        int
	ColSize        int
	BindPtr        interface{}
	LenPtr         *int
	Properties     map[string]interface{}
	NumSargs       uint
	Sargs          []*MdbSarg
	IdxSargCache   []*MdbSarg
	IsFixed        bool
	QueryOrder     int
	ColNum         int
	CurValueStart  int
	CurValueLen    int
	IsNull         bool
	CurBlobPgRow   uint32
	ChunkSize      int
	ColPrec        int
	ColScale       int
	IsLongAuto     bool
	IsUuidAuto     bool
	Props          *MdbProperties
	FixedOffset    int
	VarColNum      uint
	RowColNum      int
}

// MdbTableDef 表定义
type MdbTableDef struct {
	Entry         *MdbCatalogEntry
	Name          string
	NumCols       uint
	Columns       []*MdbColumn
	NumRows       uint
	IndexStart    int
	NumRealIdxs   uint
	NumIdxs       uint
	Indices       []*MdbIndex
	FirstDataPg   uint32
	CurPgNum      uint32
	CurPhysPg     uint32
	CurRow        uint
	NoskipDel     bool
	MapBasePg     uint32
	MapSize       int
	UsageMap      []byte
	FreemapBasePg uint32
	FreemapSize   int
	FreeUsageMap  []byte
	SargTree      *MdbSargNode
	Strategy      MdbStrategy
	ScanIdx       *MdbIndex
	MdbIdx        *MdbHandle
	Chain         *MdbIndexChain
	Props         *MdbProperties
	NumVarCols    uint
	IsTempTable   bool
	TempTablePages []interface{}
	rowFields      []MdbField
	varColOffsets  []int
}

// MdbIndex 索引定义
type MdbIndex struct {
	IndexNum    int
	Name        string
	IndexType   byte
	FirstPg     uint32
	NumRows     int
	NumKeys     uint
	KeyColNum   [MDBMaxIdxCols]int16
	KeyColOrder [MDBMaxIdxCols]byte
	Flags       byte
	Table       *MdbTableDef
}

// MdbSargNode SARG 树节点
type MdbSargNode struct {
	Op      int
	Col     *MdbColumn
	ValType byte
	Value   MdbAny
	Parent  interface{}
	Left    *MdbSargNode
	Right   *MdbSargNode
}

// MdbSarg SARG 条件
type MdbSarg struct {
	Op    int
	Value MdbAny
}

// MdbIndexPage 索引页
type MdbIndexPage struct {
	Pg        uint32
	StartPos  int
	Offset    int
	Len       int
	Rc        int
	IdxStarts [2000]uint16
	CacheValue [256]byte
}

// MdbIndexChain 索引链
type MdbIndexChain struct {
	CurDepth      int
	LastLeafFound uint32
	CleanUpMode   bool
	Pages         [MDBMaxIndexDepth]MdbIndexPage
}

// MdbField 字段值
type MdbField struct {
	Value    interface{}
	Size     int
	Start    int
	IsNull   bool
	IsFixed  bool
	ColNum   int
	Offset   int
}

// MdbBackend 数据库后端
type MdbBackend struct {
	Capabilities    uint32
	TypesTable      []*MdbBackendType
	TypeShortdate   *MdbBackendType
	TypeAutonum     *MdbBackendType
	ShortNow        string
	LongNow         string
	DateFmt         string
	ShortdateFmt    string
	CharsetStatement string
	DropStatement   string
	ConstraintNotEmpty string
	ColumnComment   string
	PerColumnComment string
	TableComment    string
	PerTableComment string
	QuoteSchemaName func(string, string) string
	CreateTableStatement string
	NormalizeCase   func(string) string
}

// MdbBackendType 后端类型
type MdbBackendType struct {
	Name              string
	NeedsPrecision    bool
	NeedsScale        bool
	NeedsByteLength   bool
	NeedsCharLength   bool
}

// Jet3 格式常量
var Jet3FormatConstants = &MdbFormatConstants{
	PgSize:             2048,
	RowCountOffset:     0x08,
	TabNumRowsOffset:   12,
	TabNumColsOffset:   25,
	TabNumIdxsOffset:   27,
	TabNumRidxsOffset:  31,
	TabUsageMapOffset:  35,
	TabFirstDpgOffset:  36,
	TabColsStartOffset: 43,
	TabRidxEntrySize:   8,
	ColFlagsOffset:     13,
	ColSizeOffset:      16,
	ColNumOffset:       1,
	TabColEntrySize:    18,
	TabFreeMapOffset:   39,
	TabColOffsetVar:    3,
	TabColOffsetFixed:  14,
	TabRowColNumOffset: 5,
	ColScaleOffset:     9,
	ColPrecOffset:      10,
}

// Jet4 格式常量
var Jet4FormatConstants = &MdbFormatConstants{
	PgSize:             4096,
	RowCountOffset:     0x0c,
	TabNumRowsOffset:   16,
	TabNumColsOffset:   45,
	TabNumIdxsOffset:   47,
	TabNumRidxsOffset:  51,
	TabUsageMapOffset:  55,
	TabFirstDpgOffset:  56,
	TabColsStartOffset: 63,
	TabRidxEntrySize:   12,
	ColFlagsOffset:     15,
	ColSizeOffset:      23,
	ColNumOffset:       5,
	TabColEntrySize:    25,
	TabFreeMapOffset:   59,
	TabColOffsetVar:    7,
	TabColOffsetFixed:  21,
	TabRowColNumOffset: 9,
	ColScaleOffset:     11,
	ColPrecOffset:      12,
}

// GetFormatConstants 根据 Jet 版本返回格式常量
func GetFormatConstants(jetVersion uint32) *MdbFormatConstants {
	if jetVersion == MDBVerJet3 {
		return Jet3FormatConstants
	}
	return Jet4FormatConstants
}

// ObjectTypeString 返回对象类型的字符串表示
func ObjectTypeString(objType int) string {
	switch objType {
	case MDBForm:
		return "Form"
	case MDBTable:
		return "Table"
	case MDBMacro:
		return "Macro"
	case MDBSystemTable:
		return "System"
	case MDBReport:
		return "Report"
	case MDBQuery:
		return "Query"
	case MDBLinkedTable:
		return "Linked"
	case MDBModule:
		return "Module"
	case MDBRelationship:
		return "Relationship"
	case MDBDatabaseProperty:
		return "Property"
	default:
		return "Unknown"
	}
}

// ColumnTypeName 返回列类型的字符串表示
func ColumnTypeName(colType int) string {
	switch colType {
	case MDBBool:
		return "Boolean"
	case MDBByte:
		return "Byte"
	case MDBInt:
		return "Integer"
	case MDBLongInt:
		return "Long Integer"
	case MDBMoney:
		return "Currency"
	case MDBFloat:
		return "Float"
	case MDBDouble:
		return "Double"
	case MDBDateTime:
		return "DateTime"
	case MDBBinary:
		return "Binary"
	case MDBText:
		return "Text"
	case MDBOle:
		return "OLE"
	case MDBMemo:
		return "Memo"
	case MDBRepId:
		return "GUID"
	case MDBNumeric:
		return "Numeric"
	case MDBComplex:
		return "Complex"
	default:
		return "Unknown"
	}
}

// IsUserTable 判断是否为用户表
func IsUserTable(entry *MdbCatalogEntry) bool {
	return entry.ObjectType == MDBTable && len(entry.ObjectName) > 0 && entry.ObjectName[0] != '~'
}

// IsSystemTable 判断是否为系统表
func IsSystemTable(entry *MdbCatalogEntry) bool {
	return entry.ObjectType == MDBSystemTable
}
