package purego

import (
	"testing"
)

// ========== 字节操作函数测试 ==========

func TestGetByte(t *testing.T) {
	tests := []struct {
		name    string
		buf     []byte
		offset  int
		want    byte
	}{
		{"正常读取", []byte{0x01, 0x02, 0x03}, 1, 0x02},
		{"首字节", []byte{0x0A, 0x0B}, 0, 0x0A},
		{"末字节", []byte{0x0A, 0x0B}, 1, 0x0B},
		{"负偏移", []byte{0x01, 0x02}, -1, 0},
		{"越界偏移", []byte{0x01, 0x02}, 5, 0},
		{"空缓冲区", []byte{}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetByte(tt.buf, tt.offset); got != tt.want {
				t.Errorf("GetByte() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetInt16(t *testing.T) {
	tests := []struct {
		name   string
		buf    []byte
		offset int
		want   int
	}{
		{"0x0102", []byte{0x02, 0x01, 0x00, 0x00}, 0, 0x0102},
		{"0x00FF", []byte{0xFF, 0x00, 0x00, 0x00}, 0, 0x00FF},
		{"带偏移", []byte{0x00, 0x00, 0x34, 0x12}, 2, 0x1234},
		{"负偏移", []byte{0x01, 0x02}, -1, 0},
		{"越界", []byte{0x01}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetInt16(tt.buf, tt.offset); got != tt.want {
				t.Errorf("GetInt16() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetInt32(t *testing.T) {
	tests := []struct {
		name   string
		buf    []byte
		offset int
		want   int
	}{
		{"0x04030201", []byte{0x01, 0x02, 0x03, 0x04}, 0, 0x04030201},
		{"0x0000FF00", []byte{0x00, 0xFF, 0x00, 0x00}, 0, 0x0000FF00},
		{"带偏移", []byte{0x00, 0x00, 0x78, 0x56, 0x34, 0x12}, 2, 0x12345678},
		{"负偏移", []byte{0x01, 0x02, 0x03, 0x04}, -1, 0},
		{"越界", []byte{0x01, 0x02}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetInt32(tt.buf, tt.offset); got != tt.want {
				t.Errorf("GetInt32() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetInt32MSB(t *testing.T) {
	tests := []struct {
		name   string
		buf    []byte
		offset int
		want   int
	}{
		{"0x01020304", []byte{0x01, 0x02, 0x03, 0x04}, 0, 0x01020304},
		{"0x12345678", []byte{0x12, 0x34, 0x56, 0x78}, 0, 0x12345678},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetInt32MSB(tt.buf, tt.offset); got != tt.want {
				t.Errorf("GetInt32MSB() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPutInt16(t *testing.T) {
	buf := make([]byte, 4)
	PutInt16(buf, 0, 0x0102)
	want := []byte{0x02, 0x01, 0x00, 0x00}
	for i := range buf {
		if buf[i] != want[i] {
			t.Errorf("PutInt16() buf[%d] = %v, want %v", i, buf[i], want[i])
		}
	}
}

func TestPutInt32(t *testing.T) {
	buf := make([]byte, 8)
	PutInt32(buf, 0, 0x04030201)
	want := []byte{0x01, 0x02, 0x03, 0x04, 0x00, 0x00, 0x00, 0x00}
	for i := range buf {
		if buf[i] != want[i] {
			t.Errorf("PutInt32() buf[%d] = %v, want %v", i, buf[i], want[i])
		}
	}
}

func TestPutInt32MSB(t *testing.T) {
	buf := make([]byte, 4)
	PutInt32MSB(buf, 0, 0x01020304)
	want := []byte{0x01, 0x02, 0x03, 0x04}
	for i := range buf {
		if buf[i] != want[i] {
			t.Errorf("PutInt32MSB() buf[%d] = %v, want %v", i, buf[i], want[i])
		}
	}
}

// ========== RC4 加密测试 ==========

func TestRC4RoundTrip(t *testing.T) {
	key := []byte{0x01, 0x02, 0x03, 0x04}
	data := []byte("Hello, World! This is a test message for RC4 encryption.")
	original := make([]byte, len(data))
	copy(original, data)

	RC4(key, data)
	for i := range data {
		if data[i] == original[i] {
			t.Fatal("RC4 encryption did not modify data")
		}
	}

	RC4(key, data)
	for i := range data {
		if data[i] != original[i] {
			t.Errorf("RC4 decrypt mismatch at %d: got %v, want %v", i, data[i], original[i])
		}
	}
}

func TestRC4String(t *testing.T) {
	key := []byte{0xC7, 0xDA, 0x39, 0x6B}
	original := "test string"
	encrypted := RC4String(key, original)
	if encrypted == original {
		t.Error("RC4String did not encrypt")
	}
	decrypted := RC4String(key, encrypted)
	if decrypted != original {
		t.Errorf("RC4String decrypt = %v, want %v", decrypted, original)
	}
}

func TestRC4SetKey(t *testing.T) {
	k := RC4SetKey([]byte{0x01, 0x02})
	if k == nil {
		t.Fatal("RC4SetKey returned nil")
	}
	if len(k.State) != 256 {
		t.Errorf("State length = %d, want 256", len(k.State))
	}
}

func TestRC4Empty(t *testing.T) {
	RC4([]byte{}, []byte{1, 2, 3})
	RC4([]byte{1}, []byte{})
}

// ========== LIKE 模式匹配测试 ==========

func TestLikeCmp(t *testing.T) {
	tests := []struct {
		s, pattern string
		want       bool
	}{
		{"hello", "hello", true},
		{"hello", "world", false},
		{"hello", "%llo", true},
		{"hello", "hel%", true},
		{"hello", "h%o", true},
		{"hello", "%", true},
		{"hello", "h_llo", true},
		{"hello", "hello_", false},
		{"abc", "[a-c]bc", true},
		{"abc", "[x-z]bc", false},
		{"abc", "[^x-z]bc", true},
		{"", "", true},
		{"abcdef", "a%c_f%", false},
	}
	for _, tt := range tests {
		t.Run(tt.s+"_"+tt.pattern, func(t *testing.T) {
			if got := LikeCmp(tt.s, tt.pattern); got != tt.want {
				t.Errorf("LikeCmp(%q, %q) = %v, want %v", tt.s, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestILikeCmp(t *testing.T) {
	if !ILikeCmp("HELLO", "hello") {
		t.Error("ILikeCmp should be case insensitive")
	}
	if !ILikeCmp("HeLLo", "%llo") {
		t.Error("ILikeCmp with wildcard failed")
	}
}

// ========== 格式转换测试 ==========

func TestDateToTm(t *testing.T) {
	tests := []struct {
		td   float64
		want string
	}{
		{1.0, "1899-12-31 00:00:00"},
		{36526.0, "2000-01-01 00:00:00"},
		{0.0, "1899-12-30 00:00:00"},
	}
	for _, tt := range tests {
		got := DateToTm(tt.td)
		if got.Format("2006-01-02 15:04:05") != tt.want {
			t.Errorf("DateToTm(%v) = %v, want %v", tt.td, got.Format("2006-01-02 15:04:05"), tt.want)
		}
	}
}

func TestUUIDToStringFmt(t *testing.T) {
	buf := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}

	got := UUIDToStringFmt(buf, 0, MDBBraces4228)
	want := "{04030201-0605-0807-090A-0B0C0D0E0F10}"
	if got != want {
		t.Errorf("UUIDToStringFmt braces = %v, want %v", got, want)
	}

	got = UUIDToStringFmt(buf, 0, MDBNoBraces42226)
	want = "04030201-0605-0807-090A-0B0C0D0E0F10"
	if got != want {
		t.Errorf("UUIDToStringFmt nobraces = %v, want %v", got, want)
	}
}

func TestColToString(t *testing.T) {
	mdb := &MdbHandle{Fmt: Jet4FormatConstants}

	tests := []struct {
		name     string
		buf      []byte
		start    int
		dataType int
		size     int
		want     string
	}{
		{"Boolean true", []byte{0x01}, 0, MDBBool, 1, "yes"},
		{"Boolean false", []byte{0x00}, 0, MDBBool, 1, "no"},
		{"Byte", []byte{0x0A}, 0, MDBByte, 1, "10"},
		{"Integer negative", []byte{0xED, 0xFF}, 0, MDBInt, 2, "-19"},
		{"Long integer negative", []byte{0xED, 0xFF, 0xFF, 0xFF}, 0, MDBLongInt, 4, "-19"},
		{"Text", []byte{0x48, 0x65, 0x6C, 0x6C, 0x6F}, 0, MDBText, 5, "Hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ColToString(mdb, tt.buf, tt.start, tt.dataType, tt.size); got != tt.want {
				t.Errorf("ColToString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestColDispSize(t *testing.T) {
	tests := []struct {
		col  *MdbColumn
		want int
	}{
		{&MdbColumn{ColType: MDBBool}, 1},
		{&MdbColumn{ColType: MDBByte}, 4},
		{&MdbColumn{ColType: MDBInt}, 6},
		{&MdbColumn{ColType: MDBLongInt}, 11},
		{&MdbColumn{ColType: MDBFloat}, 10},
		{&MdbColumn{ColType: MDBDouble}, 10},
		{&MdbColumn{ColType: MDBDateTime}, 20},
		{&MdbColumn{ColType: MDBMoney}, 21},
		{&MdbColumn{ColType: MDBText, ColSize: 50}, 50},
	}
	for _, tt := range tests {
		if got := ColDispSize(tt.col); got != tt.want {
			t.Errorf("ColDispSize(%d) = %v, want %v", tt.col.ColType, got, tt.want)
		}
	}
}

func TestColFixedSize(t *testing.T) {
	tests := []struct {
		col  *MdbColumn
		want int
	}{
		{&MdbColumn{ColType: MDBBool}, 1},
		{&MdbColumn{ColType: MDBByte}, -1},
		{&MdbColumn{ColType: MDBInt}, 2},
		{&MdbColumn{ColType: MDBLongInt}, 4},
		{&MdbColumn{ColType: MDBFloat}, 4},
		{&MdbColumn{ColType: MDBDouble}, 8},
		{&MdbColumn{ColType: MDBMoney}, 8},
	}
	for _, tt := range tests {
		if got := ColFixedSize(tt.col); got != tt.want {
			t.Errorf("ColFixedSize(%d) = %v, want %v", tt.col.ColType, got, tt.want)
		}
	}
}

func TestObjectTypeString(t *testing.T) {
	tests := []struct {
		objType int
		want    string
	}{
		{MDBForm, "Form"},
		{MDBTable, "Table"},
		{MDBQuery, "Query"},
		{MDBReport, "Report"},
		{99, "Unknown"},
	}
	for _, tt := range tests {
		if got := ObjectTypeString(tt.objType); got != tt.want {
			t.Errorf("ObjectTypeString(%d) = %v, want %v", tt.objType, got, tt.want)
		}
	}
}

func TestColumnTypeName(t *testing.T) {
	tests := []struct {
		colType int
		want    string
	}{
		{MDBBool, "Boolean"},
		{MDBByte, "Byte"},
		{MDBInt, "Integer"},
		{MDBLongInt, "Long Integer"},
		{MDBMoney, "Currency"},
		{MDBFloat, "Float"},
		{MDBDouble, "Double"},
		{MDBDateTime, "DateTime"},
		{MDBText, "Text"},
		{MDBMemo, "Memo"},
		{MDBRepId, "GUID"},
		{99, "Unknown"},
	}
	for _, tt := range tests {
		if got := ColumnTypeName(tt.colType); got != tt.want {
			t.Errorf("ColumnTypeName(%d) = %v, want %v", tt.colType, got, tt.want)
		}
	}
}

func TestGetFormatConstants(t *testing.T) {
	if f := GetFormatConstants(MDBVerJet3); f == nil || f.PgSize != 2048 {
		t.Error("Jet3 format constants incorrect")
	}
	if f := GetFormatConstants(MDBVerJet4); f == nil || f.PgSize != 4096 {
		t.Error("Jet4 format constants incorrect")
	}
}

func TestGetVersion(t *testing.T) {
	if v := GetVersion(); v != "mdbtools purego 1.0.0" {
		t.Errorf("GetVersion() = %v", v)
	}
}

func TestIsNullBit(t *testing.T) {
	tests := []struct {
		nullMask []byte
		colNum   int
		want     bool
	}{
		{[]byte{0x01}, 1, true},
		{[]byte{0x00}, 1, false},
		{[]byte{0x02}, 2, true},
		{[]byte{0x80}, 8, true},
		{[]byte{0x00, 0x01}, 9, true},
		{[]byte{0x00}, 0, true},
		{[]byte{0x00}, 100, true},
	}
	for _, tt := range tests {
		if got := IsNullBit(tt.nullMask, tt.colNum); got != tt.want {
			t.Errorf("IsNullBit(%v, %d) = %v, want %v", tt.nullMask, tt.colNum, got, tt.want)
		}
	}
}

func TestFindRowPreservesRowFlags(t *testing.T) {
	mdb := &MdbHandle{
		Fmt: GetFormatConstants(MDBVerJet4),
	}
	rowStart := 100
	PutInt16(mdb.PgBuf[:], int(mdb.Fmt.RowCountOffset)+2, rowStart|0x4000)

	gotStart, gotLength, err := mdb.FindRow(0)
	if err != nil {
		t.Fatalf("FindRow failed: %v", err)
	}
	if gotStart != rowStart|0x4000 {
		t.Fatalf("FindRow start=%#x, want deleted flag preserved in %#x", gotStart, rowStart|0x4000)
	}
	if want := mdb.Fmt.PgSize - rowStart; gotLength != want {
		t.Fatalf("FindRow length=%d, want %d", gotLength, want)
	}
}

func TestAttemptBindTracksActualLength(t *testing.T) {
	mdb := &MdbHandle{
		BooleanFalse: "0",
		BooleanTrue:  "1",
	}
	buf := make([]byte, 16)
	for i := range buf {
		buf[i] = 0x7f
	}
	length := 0
	col := &MdbColumn{
		ColType: MDBLongInt,
		BindPtr: buf,
		LenPtr:  &length,
	}

	PutInt32(mdb.PgBuf[:], 0, 12345)
	AttemptBind(mdb, nil, col, false, 0, 4)
	if got := string(buf[:length]); got != "12345" {
		t.Fatalf("first bound value = %q, want %q", got, "12345")
	}

	PutInt32(mdb.PgBuf[:], 0, 7)
	AttemptBind(mdb, nil, col, false, 0, 4)
	if got := string(buf[:length]); got != "7" {
		t.Fatalf("shorter bound value = %q, want %q", got, "7")
	}
	if buf[1] != 0 || buf[2] != 0 {
		t.Fatalf("bound value is not terminated: %x", buf[:4])
	}
	if buf[4] != '5' {
		t.Fatalf("length-aware binding unexpectedly cleared the whole buffer")
	}

	AttemptBind(mdb, nil, col, true, 0, 0)
	if length != 0 || buf[0] != 0 || buf[1] != 0 {
		t.Fatalf("NULL binding length=%d prefix=%x", length, buf[:2])
	}
}

// TestAttemptBindBooleanValue 验证 Access Boolean 位图能转换为 0/1，且 False 不会被标记为 NULL。
func TestAttemptBindBooleanValue(t *testing.T) {
	mdb := &MdbHandle{BooleanFalse: "0", BooleanTrue: "1"}
	buf := make([]byte, 4)
	length := 0
	col := &MdbColumn{ColType: MDBBool, BindPtr: buf, LenPtr: &length}

	AttemptBind(mdb, nil, col, false, 0, 0)
	if got := string(buf[:length]); got != "1" || col.IsNull {
		t.Fatalf("Boolean True binding=%q null=%v, want 1/false", got, col.IsNull)
	}

	AttemptBind(mdb, nil, col, true, 0, 0)
	if got := string(buf[:length]); got != "0" || col.IsNull {
		t.Fatalf("Boolean False binding=%q null=%v, want 0/false", got, col.IsNull)
	}
}
