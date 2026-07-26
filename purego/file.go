package purego

import (
	"fmt"
	"io"
	"math"
	"os"
	"sync"
)

// MdbFileReader 文件读取器
type MdbFileReader struct {
	File     *os.File
	Size     int64
	Mutex    sync.Mutex
}

// NewMdbFileReader 创建新的文件读取器
func NewMdbFileReader(filename string) (*MdbFileReader, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("无法打开文件 %s: %w", filename, err)
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("无法获取文件信息: %w", err)
	}

	return &MdbFileReader{
		File: file,
		Size: stat.Size(),
	}, nil
}

// Close 关闭文件
func (r *MdbFileReader) Close() error {
	return r.File.Close()
}

// ReadAt 从指定位置读取数据
func (r *MdbFileReader) ReadAt(buf []byte, offset int64) (int, error) {
	r.Mutex.Lock()
	defer r.Mutex.Unlock()

	_, err := r.File.Seek(offset, io.SeekStart)
	if err != nil {
		return 0, err
	}
	return r.File.Read(buf)
}

// GetByte 从缓冲区获取字节
func GetByte(buf []byte, offset int) byte {
	if offset < 0 || offset >= len(buf) {
		return 0
	}
	return buf[offset]
}

// GetInt16 从缓冲区获取 16 位整数（小端序）
func GetInt16(buf []byte, offset int) int {
	if offset < 0 || offset+2 > len(buf) {
		return 0
	}
	return int(uint16(buf[offset]) | uint16(buf[offset+1])<<8)
}

// GetInt32 从缓冲区获取 32 位整数（小端序）
func GetInt32(buf []byte, offset int) int {
	if offset < 0 || offset+4 > len(buf) {
		return 0
	}
	return int(uint32(buf[offset]) | uint32(buf[offset+1])<<8 |
		uint32(buf[offset+2])<<16 | uint32(buf[offset+3])<<24)
}

// GetInt32MSB 从缓冲区获取 32 位整数（大端序）
func GetInt32MSB(buf []byte, offset int) int {
	if offset < 0 || offset+4 > len(buf) {
		return 0
	}
	return int(uint32(buf[offset])<<24 | uint32(buf[offset+1])<<16 |
		uint32(buf[offset+2])<<8 | uint32(buf[offset+3]))
}

// GetSingle 从缓冲区获取单精度浮点数
func GetSingle(buf []byte, offset int) float32 {
	if offset < 0 || offset+4 > len(buf) {
		return 0
	}
	bits := uint32(buf[offset]) | uint32(buf[offset+1])<<8 |
		uint32(buf[offset+2])<<16 | uint32(buf[offset+3])<<24
	return math.Float32frombits(bits)
}

// GetDouble 从缓冲区获取双精度浮点数
func GetDouble(buf []byte, offset int) float64 {
	if offset < 0 || offset+8 > len(buf) {
		return 0
	}
	bits := uint64(buf[offset]) | uint64(buf[offset+1])<<8 |
		uint64(buf[offset+2])<<16 | uint64(buf[offset+3])<<24 |
		uint64(buf[offset+4])<<32 | uint64(buf[offset+5])<<40 |
		uint64(buf[offset+6])<<48 | uint64(buf[offset+7])<<56
	return math.Float64frombits(bits)
}

// PutInt16 写入 16 位整数到缓冲区（小端序）
func PutInt16(buf []byte, offset int, value int) {
	if offset < 0 || offset+2 > len(buf) {
		return
	}
	buf[offset] = byte(value)
	buf[offset+1] = byte(value >> 8)
}

// PutInt32 写入 32 位整数到缓冲区（小端序）
func PutInt32(buf []byte, offset int, value int) {
	if offset < 0 || offset+4 > len(buf) {
		return
	}
	buf[offset] = byte(value)
	buf[offset+1] = byte(value >> 8)
	buf[offset+2] = byte(value >> 16)
	buf[offset+3] = byte(value >> 24)
}

// PutInt32MSB 写入 32 位整数到缓冲区（大端序）
func PutInt32MSB(buf []byte, offset int, value int) {
	if offset < 0 || offset+4 > len(buf) {
		return
	}
	buf[offset] = byte(value >> 24)
	buf[offset+1] = byte(value >> 16)
	buf[offset+2] = byte(value >> 8)
	buf[offset+3] = byte(value)
}

// PgGetByte 从页面缓冲区获取字节
func (mdb *MdbHandle) PgGetByte(offset int) byte {
	if offset < 0 || offset+1 > mdb.Fmt.PgSize {
		return 0
	}
	mdb.CurPos++
	return mdb.PgBuf[offset]
}

// PgGetInt16 从页面缓冲区获取 16 位整数
func (mdb *MdbHandle) PgGetInt16(offset int) int {
	if offset < 0 || offset+2 > mdb.Fmt.PgSize {
		return -1
	}
	mdb.CurPos += 2
	return GetInt16(mdb.PgBuf[:], offset)
}

// PgGetInt32 从页面缓冲区获取 32 位整数
func (mdb *MdbHandle) PgGetInt32(offset int) int {
	if offset < 0 || offset+4 > mdb.Fmt.PgSize {
		return -1
	}
	mdb.CurPos += 4
	return GetInt32(mdb.PgBuf[:], offset)
}

// PgGetSingle 从页面缓冲区获取单精度浮点数
func (mdb *MdbHandle) PgGetSingle(offset int) float32 {
	if offset < 0 || offset+4 > mdb.Fmt.PgSize {
		return -1
	}
	mdb.CurPos += 4
	return GetSingle(mdb.PgBuf[:], offset)
}

// PgGetDouble 从页面缓冲区获取双精度浮点数
func (mdb *MdbHandle) PgGetDouble(offset int) float64 {
	if offset < 0 || offset+8 > mdb.Fmt.PgSize {
		return -1
	}
	mdb.CurPos += 8
	return GetDouble(mdb.PgBuf[:], offset)
}

// ReadPg 读取指定页面
func (mdb *MdbHandle) ReadPg(pg uint32) int {
	if pg != 0 && mdb.CurPg == pg {
		return mdb.Fmt.PgSize
	}

	n := mdb.readPgBuf(mdb.PgBuf[:], pg)
	if n == 0 {
		return 0
	}

	mdb.CurPg = pg
	mdb.CurPos = 0
	return n
}

// ReadAltPg 读取备用页面
func (mdb *MdbHandle) ReadAltPg(pg uint32) int {
	return mdb.readPgBuf(mdb.AltPgBuf[:], pg)
}

// readPgBuf 读取页面到指定缓冲区
func (mdb *MdbHandle) readPgBuf(buf []byte, pg uint32) int {
	if mdb.F == nil || mdb.F.Reader == nil {
		return 0
	}

	offset := int64(pg) * int64(mdb.Fmt.PgSize)

	// 检查是否超出文件范围
	if offset >= mdb.F.Reader.Size {
		return 0
	}

	// 读取页面数据
	n, err := mdb.F.Reader.ReadAt(buf[:mdb.Fmt.PgSize], offset)
	if err != nil && n == 0 {
		return 0
	}

	// 更新统计信息
	if mdb.Stats != nil && mdb.Stats.Collect {
		mdb.Stats.PgReads++
	}

	// 如果读取的数据不足页面大小，用零填充
	if n < mdb.Fmt.PgSize {
		for i := n; i < mdb.Fmt.PgSize; i++ {
			buf[i] = 0
		}
	}

	// 解密页面（如果需要）
	if pg != 0 && mdb.F.DBKey != 0 {
		tmpKey := mdb.F.DBKey ^ uint32(pg)
		key := []byte{
			byte(tmpKey),
			byte(tmpKey >> 8),
			byte(tmpKey >> 16),
			byte(tmpKey >> 24),
		}
		RC4(key, buf[:mdb.Fmt.PgSize])
	}

	return mdb.Fmt.PgSize
}

// SwapPgBuf 交换主缓冲区和备用缓冲区
func (mdb *MdbHandle) SwapPgBuf() {
	mdb.PgBuf, mdb.AltPgBuf = mdb.AltPgBuf, mdb.PgBuf
}

// SetPos 设置当前位置
func (mdb *MdbHandle) SetPos(pos int) int {
	if pos < 0 || pos >= mdb.Fmt.PgSize {
		return 0
	}
	mdb.CurPos = pos
	return pos
}

// GetPos 获取当前位置
func (mdb *MdbHandle) GetPos() int {
	return mdb.CurPos
}

// Open 打开 MDB 文件
func Open(filename string, flags MdbFileFlags) (*MdbHandle, error) {
	reader, err := NewMdbFileReader(filename)
	if err != nil {
		return nil, err
	}

	mdb := &MdbHandle{
		F: &MdbFile{
			Reader:    reader,
			Writable:  flags&MDBWritable != 0,
			Refs:      1,
			FreeMap:   make([]byte, 0),
			Stats:     &MdbStatistics{},
		},
		Fmt:        Jet3FormatConstants, // 先使用 Jet3 格式
		DateFmt:    "%x %X",
		ShortDateFmt: "%x",
		BindSize:   MDBBindSize,
		Catalog:    make([]*MdbCatalogEntry, 0),
		Backends:   make(map[string]*MdbBackend),
	}

	// 读取第 0 页
	if mdb.ReadPg(0) == 0 {
		mdb.Close()
		return nil, fmt.Errorf("无法读取第 0 页")
	}

	// 检查页面类型
	if mdb.PgBuf[0] != 0 {
		mdb.Close()
		return nil, fmt.Errorf("无效的 MDB 文件格式")
	}

	// 获取 Jet 版本
	mdb.F.JetVersion = uint32(GetByte(mdb.PgBuf[:], 0x14))

	// 根据版本设置格式常量
	switch mdb.F.JetVersion {
	case MDBVerJet3:
		mdb.Fmt = Jet3FormatConstants
	case MDBVerJet4, MDBVerAccdb2007, MDBVerAccdb2010, MDBVerAccdb2013, MDBVerAccdb2016, MDBVerAccdb2019:
		mdb.Fmt = Jet4FormatConstants
	default:
		mdb.Close()
		return nil, fmt.Errorf("未知的 Jet 版本: %x", mdb.F.JetVersion)
	}

	// 解密数据库密钥
	tmpKey := []byte{0xC7, 0xDA, 0x39, 0x6B}
	dataLen := 126
	if mdb.F.JetVersion != MDBVerJet3 {
		dataLen = 128
	}
	RC4(tmpKey, mdb.PgBuf[0x18:0x18+dataLen])

	// 获取语言 ID 和代码页
	if mdb.F.JetVersion == MDBVerJet3 {
		mdb.F.LangID = uint16(GetInt16(mdb.PgBuf[:], 0x3a))
	} else {
		mdb.F.LangID = uint16(GetInt16(mdb.PgBuf[:], 0x6e))
	}
	mdb.F.CodePage = uint16(GetInt16(mdb.PgBuf[:], 0x3c))
	mdb.F.DBKey = uint32(GetInt32(mdb.PgBuf[:], 0x3e))

	// 获取密码（Jet3）
	if mdb.F.JetVersion == MDBVerJet3 {
		copy(mdb.F.DBPasswd[:], mdb.PgBuf[0x42:0x42+14])
	}

	return mdb, nil
}

// OpenBuffer 从内存缓冲区打开 MDB 文件
func OpenBuffer(buffer []byte, flags MdbFileFlags) (*MdbHandle, error) {
	// 创建一个临时文件来存储缓冲区数据
	tmpFile, err := os.CreateTemp("", "mdb_*.tmp")
	if err != nil {
		return nil, fmt.Errorf("无法创建临时文件: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// 写入缓冲区数据
	_, err = tmpFile.Write(buffer)
	if err != nil {
		return nil, fmt.Errorf("无法写入临时文件: %w", err)
	}

	// 关闭文件以便重新打开
	tmpFile.Close()

	// 重新打开文件
	reader, err := NewMdbFileReader(tmpFile.Name())
	if err != nil {
		return nil, err
	}

	mdb := &MdbHandle{
		F: &MdbFile{
			Reader:    reader,
			Writable:  flags&MDBWritable != 0,
			Refs:      1,
			FreeMap:   make([]byte, 0),
			Stats:     &MdbStatistics{},
		},
		Fmt:        Jet3FormatConstants,
		DateFmt:    "%x %X",
		ShortDateFmt: "%x",
		BindSize:   MDBBindSize,
		Catalog:    make([]*MdbCatalogEntry, 0),
		Backends:   make(map[string]*MdbBackend),
	}

	// 读取第 0 页
	if mdb.ReadPg(0) == 0 {
		mdb.Close()
		return nil, fmt.Errorf("无法读取第 0 页")
	}

	// 检查页面类型
	if mdb.PgBuf[0] != 0 {
		mdb.Close()
		return nil, fmt.Errorf("无效的 MDB 文件格式")
	}

	// 获取 Jet 版本
	mdb.F.JetVersion = uint32(GetByte(mdb.PgBuf[:], 0x14))

	// 根据版本设置格式常量
	switch mdb.F.JetVersion {
	case MDBVerJet3:
		mdb.Fmt = Jet3FormatConstants
	case MDBVerJet4, MDBVerAccdb2007, MDBVerAccdb2010, MDBVerAccdb2013, MDBVerAccdb2016, MDBVerAccdb2019:
		mdb.Fmt = Jet4FormatConstants
	default:
		mdb.Close()
		return nil, fmt.Errorf("未知的 Jet 版本: %x", mdb.F.JetVersion)
	}

	// 解密数据库密钥
	tmpKey := []byte{0xC7, 0xDA, 0x39, 0x6B}
	dataLen := 126
	if mdb.F.JetVersion != MDBVerJet3 {
		dataLen = 128
	}
	RC4(tmpKey, mdb.PgBuf[0x18:0x18+dataLen])

	// 获取语言 ID 和代码页
	if mdb.F.JetVersion == MDBVerJet3 {
		mdb.F.LangID = uint16(GetInt16(mdb.PgBuf[:], 0x3a))
	} else {
		mdb.F.LangID = uint16(GetInt16(mdb.PgBuf[:], 0x6e))
	}
	mdb.F.CodePage = uint16(GetInt16(mdb.PgBuf[:], 0x3c))
	mdb.F.DBKey = uint32(GetInt32(mdb.PgBuf[:], 0x3e))

	// 获取密码（Jet3）
	if mdb.F.JetVersion == MDBVerJet3 {
		copy(mdb.F.DBPasswd[:], mdb.PgBuf[0x42:0x42+14])
	}

	return mdb, nil
}

// Close 关闭数据库
func (mdb *MdbHandle) Close() {
	if mdb == nil {
		return
	}

	// 释放目录
	mdb.FreeCatalog()

	// 释放统计信息
	if mdb.Stats != nil {
		mdb.Stats = nil
	}

	// 释放后端名称
	mdb.BackendName = ""

	// 关闭文件
	if mdb.F != nil {
		if mdb.F.Refs > 1 {
			mdb.F.Refs--
		} else {
			if mdb.F.Reader != nil {
				mdb.F.Reader.Close()
			}
			mdb.F = nil
		}
	}

	// 移除后端
	mdb.RemoveBackends()
}

// CloneHandle 克隆数据库句柄
func (mdb *MdbHandle) CloneHandle() *MdbHandle {
	if mdb == nil {
		return nil
	}

	newMdb := &MdbHandle{
		F:           mdb.F,
		CurPg:       mdb.CurPg,
		RowNum:      mdb.RowNum,
		CurPos:      mdb.CurPos,
		Fmt:         mdb.Fmt,
		BindSize:    mdb.BindSize,
		DateFmt:     mdb.DateFmt,
		ShortDateFmt: mdb.ShortDateFmt,
		RepidFmt:    mdb.RepidFmt,
		BooleanFalse: mdb.BooleanFalse,
		BooleanTrue: mdb.BooleanTrue,
		Catalog:     make([]*MdbCatalogEntry, 0),
		Backends:    make(map[string]*MdbBackend),
	}

	// 复制目录
	for _, entry := range mdb.Catalog {
		newEntry := &MdbCatalogEntry{
			Mdb:        newMdb,
			ObjectName: entry.ObjectName,
			ObjectType: entry.ObjectType,
			TablePg:    entry.TablePg,
			Props:      make([]*MdbProperties, 0),
			Flags:      entry.Flags,
		}
		newMdb.Catalog = append(newMdb.Catalog, newEntry)
	}

	// 增加文件引用计数
	if mdb.F != nil {
		mdb.F.Refs++
	}

	// 设置默认后端
	if mdb.BackendName != "" {
		newMdb.SetDefaultBackend(mdb.BackendName)
	}

	return newMdb
}

// GetFileFormat 获取文件格式信息
func (mdb *MdbHandle) GetFileFormat() (jetVersion uint32, pageSize int) {
	if mdb.F == nil {
		return 0, 0
	}
	return mdb.F.JetVersion, mdb.Fmt.PgSize
}

// SetDateFmt 设置日期格式
func (mdb *MdbHandle) SetDateFmt(fmt string) {
	mdb.DateFmt = fmt
}

// SetShortDateFmt 设置短日期格式
func (mdb *MdbHandle) SetShortDateFmt(fmt string) {
	mdb.ShortDateFmt = fmt
}

// SetRepidFmt 设置 UUID 格式
func (mdb *MdbHandle) SetRepidFmt(fmt MdbUuidFormat) {
	mdb.RepidFmt = fmt
}

// SetBooleanFmtNumbers 设置布尔值格式为数字
func (mdb *MdbHandle) SetBooleanFmtNumbers() {
	mdb.BooleanFalse = "0"
	mdb.BooleanTrue = "1"
}

// SetBooleanFmtWords 设置布尔值格式为单词
func (mdb *MdbHandle) SetBooleanFmtWords() {
	mdb.BooleanFalse = "False"
	mdb.BooleanTrue = "True"
}

// SetBindSize 设置绑定大小
func (mdb *MdbHandle) SetBindSize(size int) {
	mdb.BindSize = size
}

// StatsOn 开启统计
func (mdb *MdbHandle) StatsOn() {
	if mdb.Stats == nil {
		mdb.Stats = &MdbStatistics{}
	}
	mdb.Stats.Collect = true
}

// StatsOff 关闭统计
func (mdb *MdbHandle) StatsOff() {
	if mdb.Stats != nil {
		mdb.Stats.Collect = false
	}
}

// DumpStats 输出统计信息
func (mdb *MdbHandle) DumpStats() {
	if mdb.Stats != nil {
		fmt.Printf("页面读取次数: %d\n", mdb.Stats.PgReads)
	}
}

// GetVersion 获取版本字符串
func GetVersion() string {
	return "mdbtools purego 1.0.0"
}

// BufferDump 调试输出缓冲区内容
func BufferDump(buf []byte, start int, length int) {
	if start < 0 {
		start = 0
	}
	if length <= 0 || start+length > len(buf) {
		length = len(buf) - start
	}

	for i := 0; i < length; i += 16 {
		fmt.Printf("%04x: ", start+i)
		for j := 0; j < 16 && i+j < length; j++ {
			fmt.Printf("%02x ", buf[start+i+j])
		}
		for j := length - i; j < 16; j++ {
			fmt.Printf("   ")
		}
		fmt.Printf(" ")
		for j := 0; j < 16 && i+j < length; j++ {
			c := buf[start+i+j]
			if c >= 32 && c < 127 {
				fmt.Printf("%c", c)
			} else {
				fmt.Printf(".")
			}
		}
		fmt.Println()
	}
}

// GetOption 获取调试选项
func GetOption(optnum uint) int {
	return 0
}

// Debug 调试输出
func Debug(klass int, format string, args ...interface{}) {
	// 在 Go 中，我们可以使用 log 包或者简单地忽略调试输出
	// 这里我们选择简单地忽略
}
