package mdbgo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
)

// ControlTypeCodeToString 把 Access 控件类型代码映射为可读类型名。
//
// 代码来源与你给出的 VBA 对照表，便于后续二进制解析直接复用。
func ControlTypeCodeToString(ctype int) string {
	switch ctype {
	case 109:
		return "TextBox"
	case 108:
		return "Label"
	case 104:
		return "Button"
	case 111:
		return "ComboBox"
	case 110:
		return "ListBox"
	case 106:
		return "CheckBox"
	case 105:
		return "OptionButton"
	case 107:
		return "OptionGroup"
	case 112:
		return "SubForm"
	case 114:
		return "ObjectFrame"
	case 100:
		return "Line"
	case 101:
		return "Rectangle"
	case 119:
		return "CustomControl"
	case 118:
		return "TabControl"
	default:
		return fmt.Sprintf("Unknown_%d", ctype)
	}
}

// FormControlInfo 是从 TypeInfo 流解析出的控件目录项。
type FormControlInfo struct {
	Name     string
	Type     string
	TypeCode uint16
	Index    uint32
}

// FormProperty 是从 Blob 流解析出的窗体或控件属性。
type FormProperty struct {
	ID        uint16
	Name      string
	ValueType string
	Value     string
	RawValue  []byte
}

// FormControlContent 是一个控件及其设计属性。
type FormControlContent struct {
	Name             string
	Type             string
	TypeCode         uint16
	Index            uint32
	BlobOffset       int
	Section          string
	IsSection        bool
	Left             int
	Top              int
	Width            int
	Height           int
	HasGeometry      bool
	Caption          string
	Picture          string
	ControlTipText   string
	OnClick          string
	ControlSource    string
	SourceObject     string
	LinkChildFields  string
	LinkMasterFields string
	EventProcPrefix  string
	Format           string
	Tag              string
	FontName         string
	FontSize         int
	FontWeight       int
	Underline        bool
	StatusBarText    string
	RowSourceType    string
	RowSource        string
	ColumnWidths     string
	ColumnCount      int
	ListRows         int
	ListWidth        int
	BoundColumn      int
	Locked           bool
	CanShrink        bool
	Visible          bool
	TextAlign        string
	TextAlignValue   byte
	TabIndex         int
	BackStyle        int
	BackColor        string
	BackColorValue   uint32
	ForeColor        string
	ForeColorValue   uint32
	BackGroundColor  string
	BorderStyle      int
	BorderWidth      int
	BorderColor      string
	BorderColorValue uint32
	SpecialEffect    int
	OptionValue      int
	PageIndex        int
	Properties       []FormProperty
}

// FormSectionContent 是 Access 窗体的一个设计分区。
// 常见分区为 FormHeader、Detail 和 FormFooter；Controls 不包含分区标记本身。
type FormSectionContent struct {
	Name            string
	Type            string
	TypeCode        uint16
	Index           uint32
	Height          int
	BackColor       string
	BackColorValue  uint32
	BackGroundColor string
	SpecialEffect   int
	Visible         bool
	EventProcPrefix string
	Tag             string
	Properties      []FormProperty
	Controls        []FormControlContent
}

// ParseFormTypeInfo 解析窗体 TypeInfo 流中的控件名、类型和索引。
func ParseFormTypeInfo(data []byte) ([]FormControlInfo, error) {
	return parseFormTypeInfo(data, false)
}

func parseFormTypeInfo(data []byte, includeInternal bool) ([]FormControlInfo, error) {
	const typeInfoMagic = uint32(0xACCDEAF7)
	if len(data) < 32 {
		return nil, errors.New("TypeInfo is too short")
	}
	if le32(data) != typeInfoMagic {
		return nil, fmt.Errorf("TypeInfo magic mismatch: 0x%08X", le32(data))
	}
	count := int(le32(data[12:]))
	if count < 0 || count > (len(data)-32)/8 {
		return nil, fmt.Errorf("invalid TypeInfo control count: %d", count)
	}

	controls := make([]FormControlInfo, 0, count)
	pos := 32
	for i := 0; i < count; i++ {
		if pos+8 > len(data) {
			return nil, fmt.Errorf("truncated TypeInfo entry %d", i)
		}
		typeCode := le16(data[pos:])
		index := le32(data[pos+4:])
		pos += 8

		eventName, next, ok := readNullTerminatedBytes(data, pos)
		if !ok {
			return nil, fmt.Errorf("truncated TypeInfo event name at entry %d", i)
		}
		pos = next
		realName, next, ok := readNullTerminatedBytes(data, pos)
		if !ok {
			return nil, fmt.Errorf("truncated TypeInfo real name at entry %d", i)
		}
		pos = next

		nameBytes := eventName
		if len(realName) > 0 {
			nameBytes = realName
		}
		if typeCode == 0x1EFF && !includeInternal {
			continue
		}
		name := decodeShiftJIS(nameBytes)
		controls = append(controls, FormControlInfo{
			Name:     name,
			Type:     FormControlTypeCodeToString(typeCode),
			TypeCode: typeCode,
			Index:    index,
		})
	}
	return controls, nil
}

// FormControlTypeCodeToString 把 TypeInfo 中的内部控件代码转换为类型名。
func FormControlTypeCodeToString(typeCode uint16) string {
	switch typeCode {
	case 0x066A:
		return "CheckBox"
	case 0x0769:
		return "OptionButton"
	case 0x0A7A:
		return "ToggleButton"
	case 0x0B68:
		return "Button"
	case 0x0C64, 0x0D64, 0x1B64:
		return "Label"
	case 0x0E65, 0x1B65:
		return "Rectangle"
	case 0x0F67, 0x1B67:
		return "Image"
	case 0x116B:
		return "OptionGroup"
	case 0x126D, 0x1B6D:
		return "TextBox"
	case 0x136F, 0x1B6F:
		return "ComboBox"
	case 0x1470:
		return "SubForm"
	case 0x1B70:
		return "SubReport"
	case 0x1666, 0x1B66:
		return "Line"
	case 0x1898, 0x1998:
		return "Detail"
	case 0x1899:
		return "FormHeader"
	case 0x189A:
		return "FormFooter"
	case 0x1999:
		return "ReportHeader"
	case 0x199A:
		return "ReportFooter"
	case 0x199D:
		return "GroupHeader"
	case 0x199E:
		return "GroupFooter"
	case 0x1F9B:
		return "PageHeader"
	case 0x1F9C:
		return "PageFooter"
	case 0x207B:
		return "TabControl"
	case 0x217C:
		return "TabPage"
	case 0x247F:
		return "EmptyCell"
	default:
		return fmt.Sprintf("Unknown_0x%04X", typeCode)
	}
}

// FormPropertyIDToName 返回已识别的 Blob 属性名。
func FormPropertyIDToName(id uint16) string {
	switch id {
	case 0x0007:
		return "Picture"
	case 0x000D:
		return "BoundColumn"
	case 0x0004:
		return "SpecialEffect"
	case 0x0008:
		return "BorderColor"
	case 0x0009:
		return "BorderStyle"
	case 0x000A:
		return "BorderWidth"
	case 0x000B:
		return "BorderLineStyle"
	case 0x000E:
		return "CanGrow"
	case 0x0010:
		return "CanShrink"
	case 0x0011:
		return "Caption"
	case 0x0012:
		return "ColumnWidths"
	case 0x0014:
		return "Name"
	case 0x0015:
		return "ControlType"
	case 0x0016:
		return "EventProcPrefix"
	case 0x0017:
		return "DefaultValue"
	case 0x0019:
		return "Enabled"
	case 0x001B:
		return "ControlSource"
	case 0x001C:
		return "BackColor"
	case 0x001D:
		return "BackStyle"
	case 0x0020:
		return "FontBold"
	case 0x0021:
		return "FontItalic"
	case 0x0022, 0x00A0:
		return "FontName"
	case 0x0023:
		return "FontSize"
	case 0x0024:
		return "FontUnderline"
	case 0x0025:
		return "FontWeight"
	case 0x0026:
		return "Format"
	case 0x002C:
		return "Height"
	case 0x0031:
		return "LinkChildFields"
	case 0x0032:
		return "LinkMasterFields"
	case 0x0036:
		return "Left"
	case 0x0038:
		return "Locked"
	case 0x003A:
		return "OptionValue"
	case 0x0045:
		return "HideDuplicates"
	case 0x0046:
		return "ColumnCount"
	case 0x0047:
		return "DecimalPlaces"
	case 0x0048:
		return "InputMask"
	case 0x005B:
		return "RowSource"
	case 0x005D:
		return "RowSourceType"
	case 0x0068:
		return "OnKeyDown"
	case 0x0069:
		return "OnKeyUp"
	case 0x006A:
		return "OnKeyPress"
	case 0x006B:
		return "OnMouseDown"
	case 0x006C:
		return "OnMouseUp"
	case 0x006D:
		return "OnMouseMove"
	case 0x0073:
		return "OnGotFocus"
	case 0x0074:
		return "OnLostFocus"
	case 0x007E:
		return "OnClick"
	case 0x0082:
		return "RunningSum"
	case 0x0084:
		return "SourceObject"
	case 0x0087:
		return "StatusBarText"
	case 0x0088:
		return "TextAlign"
	case 0x008D:
		return "Top"
	case 0x0094:
		return "Visible"
	case 0x0096:
		return "Width"
	case 0x0098:
		return "ScrollBars"
	case 0x0099:
		return "ListRows"
	case 0x009A:
		return "ListWidth"
	case 0x009C:
		return "RecordSource"
	case 0x00CC:
		return "ForeColor"
	case 0x00DE:
		return "OnEnter"
	case 0x00DF:
		return "OnExit"
	case 0x00E0:
		return "OnDblClick"
	case 0x00F5:
		return "Filter"
	case 0x00ED:
		return "Section"
	case 0x0105:
		return "TabIndex"
	case 0x0106:
		return "TabStop"
	case 0x010A:
		return "Tag"
	case 0x013D:
		return "ControlTipText"
	case 0x0160:
		return "PageIndex"
	case 0x01DC:
		return "TextFormat"
	default:
		return fmt.Sprintf("0x%04X", id)
	}
}

func le16(b []byte) uint16 {
	if len(b) < 2 {
		return 0
	}
	return uint16(b[0]) | uint16(b[1])<<8
}

func le32(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func readNullTerminatedBytes(data []byte, pos int) ([]byte, int, bool) {
	if pos < 0 || pos >= len(data) {
		return nil, pos, false
	}
	end := bytes.IndexByte(data[pos:], 0)
	if end < 0 {
		return nil, pos, false
	}
	end += pos
	return data[pos:end], end + 1, true
}

func decodeShiftJIS(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	decoded, err := japanese.ShiftJIS.NewDecoder().Bytes(data)
	if err != nil {
		return string(data)
	}
	return string(decoded)
}

func parseFormBlob(data []byte) ([]FormProperty, [][]FormProperty) {
	if len(data) < 14 {
		return nil, nil
	}

	formProps := make([]FormProperty, 0)
	pos := 14
	for pos+14 <= len(data) {
		prop, next, ok := parseFormBlobPropertyAt(data, pos, len(data))
		if !ok {
			break
		}
		formProps = append(formProps, prop)
		pos = next
	}

	namePattern := []byte{0x14, 0x00, 0x0A, 0x00, 0x00, 0x00}
	sectionStarts := make([]int, 0)
	for searchPos := pos; searchPos+len(namePattern) <= len(data); {
		rel := bytes.Index(data[searchPos:], namePattern)
		if rel < 0 {
			break
		}
		start := searchPos + rel
		sectionStarts = append(sectionStarts, start)
		searchPos = start + len(namePattern)
	}

	groups := make([][]FormProperty, 0, len(sectionStarts))
	for i, start := range sectionStarts {
		end := len(data)
		if i+1 < len(sectionStarts) {
			end = sectionStarts[i+1]
		}
		props := make([]FormProperty, 0)
		for p := start; p+14 <= end; {
			prop, next, ok := parseFormBlobPropertyAt(data, p, end)
			if !ok {
				break
			}
			props = append(props, prop)
			p = next
		}
		if len(props) > 0 {
			groups = append(groups, props)
		}
	}
	return formProps, groups
}

// parseJet4FormTextProperties 解析 Jet4 MDB Blob 中按控件顺序保存的文本属性。
// Jet4 的布局区与 ACE Blob 属性项格式不同，但控件块仍按 TypeInfo 顺序排列，
// 每块以控件名开头，随后保存 ControlSource、Caption、Format、FontName 等文本。
func parseJet4FormTextProperties(data []byte, controls []FormControlInfo) ([]FormProperty, map[string][]FormProperty) {
	result := make(map[string][]FormProperty, len(controls))
	if len(data) < 8 || len(controls) == 0 || le16(data) > 0x0014 {
		return nil, result
	}

	offsets := orderedFormControlOffsets(data, controls)
	firstControlOffset := len(data)
	for _, off := range offsets {
		if off >= 0 && off < firstControlOffset {
			firstControlOffset = off
		}
	}

	var formProps []FormProperty
	if firstControlOffset > 0 && firstControlOffset <= len(data) {
		prefix := data[:firstControlOffset]
		prefixFields := scanJet4TaggedTextFields(prefix)
		for _, field := range prefixFields {
			if field.Tag != 0xDC {
				continue
			}
			if source, ok := normalizeTaggedRecordSource(field.Value); ok {
				formProps = append(formProps, newTextFormProperty(0x009C, source))
				break
			}
		}
		for _, field := range prefixFields {
			if len(formProps) > 0 {
				break
			}
			if source, ok := normalizeRecordSource(field.Value); ok {
				formProps = append(formProps, newTextFormProperty(0x009C, source))
				break
			}
		}
		if len(formProps) == 0 {
			for _, value := range extractUTF16LEWords(prefix, 2) {
				if source, ok := normalizeRecordSource(value); ok {
					formProps = append(formProps, newTextFormProperty(0x009C, source))
					break
				}
			}
		}
		for _, field := range prefixFields {
			if field.Tag == 0xDD && strings.TrimSpace(field.Value) != "" {
				formProps = mergeFormProperties(formProps, []FormProperty{newTextFormProperty(0x0011, field.Value)})
				break
			}
		}
	}

	for i, control := range controls {
		start := offsets[i]
		if start < 0 {
			continue
		}
		fields := parseJet4TaggedTextFieldsForType(data[start:], control.Name, control.Type)
		var props []FormProperty
		for _, field := range fields {
			switch {
			case strings.EqualFold(field.Value, control.Name):
				props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0014, control.Name)})
			case isKnownFormFont(field.Value):
				props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0022, field.Value)})
			}
		}
		props = mergeFormProperties(props, parseJet4ComponentTextProperties(control, fields))
		if control.Type == "Button" {
			props = mergeFormProperties(props, parseJet4ButtonBinaryProperties(
				control, jet4FormControlBlock(data, offsets, i)))
		}
		if len(props) > 0 {
			result[strings.ToLower(control.Name)] = props
		}
	}
	return formProps, result
}

func parseJet4ComponentTextProperties(control FormControlInfo, fields []jet4TaggedTextField) []FormProperty {
	switch control.Type {
	case "TextBox":
		return parseJet4TextBoxTextProperties(control, fields)
	case "Label":
		return parseJet4LabelTextProperties(control, fields)
	case "Button":
		return parseJet4ButtonTextProperties(control, fields)
	case "ToggleButton":
		return parseJet4ToggleButtonTextProperties(control, fields)
	case "ComboBox":
		return parseJet4ComboBoxTextProperties(control, fields)
	case "CheckBox":
		return parseJet4CheckBoxTextProperties(control, fields)
	case "OptionGroup":
		return parseJet4OptionGroupTextProperties(control, fields)
	case "OptionButton":
		return parseJet4OptionButtonTextProperties(control, fields)
	case "SubForm":
		return parseJet4SubFormTextProperties(control, fields)
	case "TabControl":
		return parseJet4TabControlTextProperties(control, fields)
	case "TabPage":
		return parseJet4TabPageTextProperties(control, fields)
	default:
		return nil
	}
}

type formControlGeometry struct {
	Left   int
	Top    int
	Width  int
	Height int
}

func accessTextAlignName(value byte) string {
	switch value {
	case 2:
		return "center"
	case 3:
		return "right"
	default:
		return "left"
	}
}

func jet4FormControlBlock(data []byte, offsets []int, index int) []byte {
	if index < 0 || index >= len(offsets) || offsets[index] < 0 {
		return nil
	}
	start := offsets[index]
	end := len(data)
	for _, offset := range offsets {
		if offset > start && offset < end {
			end = offset
		}
	}
	return data[start:end]
}

func jet4ControlNumericTail(block []byte, controlName string) []byte {
	return jet4ControlNumericTailForType(block, controlName, "")
}

func jet4ControlNumericTailForType(block []byte, controlName, controlType string) []byte {
	nameBytes := encodeUTF16LE(controlName)
	if len(nameBytes) == 0 || !hasUTF16LEPrefixFoldASCII(block, nameBytes) {
		return nil
	}
	for pos := len(nameBytes); pos+2 <= len(block); {
		tag := block[pos]
		if tag == 0xFD || tag == 0xFE || tag == 0xFF {
			// 部分控件（例如 ComboBox）不保存对象 GUID，最后一个文本属性后
			// 会直接跟下一个控件的紧凑数值记录。
			return block[pos:]
		}
		byteLen := int(block[pos+1])
		if byteLen == 16 && isJet4ControlGUIDTagForType(tag, controlType) && pos+18 <= len(block) {
			return block[pos+18:]
		}
		if tag < 0xC0 || byteLen < 2 || byteLen%2 != 0 || pos+2+byteLen > len(block) {
			return nil
		}
		pos += 2 + byteLen
	}
	return nil
}

func accessColorHex(value uint32) string {
	return fmt.Sprintf("#%02x%02x%02x", value&0xFF, (value>>8)&0xFF, (value>>16)&0xFF)
}

// parseJet4FormGeometries 解析 Jet4 紧凑控件块中的 twips 坐标。
// TextBox 会由具备类型特征的数值记录覆盖；此处保留块本身的布局解析，
// 作为其他控件类型在完成独立特征解析前的兼容路径。
func parseJet4FormGeometries(data []byte, controls []FormControlInfo) (int, map[string]formControlGeometry) {
	result := make(map[string]formControlGeometry, len(controls))
	if len(data) < 8 || len(controls) == 0 || le16(data) > 0x0014 {
		return 0, result
	}

	offsets := orderedFormControlOffsets(data, controls)
	for i := range controls {
		block := jet4FormControlBlock(data, offsets, i)
		if len(block) == 0 {
			continue
		}
		geometry, ok := parseJet4ControlGeometry(block)
		if !ok {
			continue
		}
		result[strings.ToLower(controls[i].Name)] = geometry
	}
	return parseJet4FormWidth(data), result
}

func parseJet4ControlGeometry(block []byte) (formControlGeometry, bool) {
	var result formControlGeometry
	found := false
	for pos := 0; pos+12 <= len(block); pos++ {
		if block[pos] != 0x60 || block[pos+3] != 0x61 ||
			block[pos+6] != 0x62 || block[pos+9] != 0x63 {
			continue
		}
		candidate := formControlGeometry{
			Left:   int(le16(block[pos+1:])),
			Top:    int(le16(block[pos+4:])),
			Width:  int(le16(block[pos+7:])),
			Height: int(le16(block[pos+10:])),
		}
		if candidate.Left > 32767 || candidate.Top > 32767 ||
			candidate.Width <= 0 || candidate.Width > 32767 ||
			candidate.Height <= 0 || candidate.Height > 32767 {
			continue
		}
		// 图片按钮的二进制图像中理论上也可能偶遇同样字节；真实布局标签位于块尾，保留最后一个有效序列。
		result = candidate
		found = true
	}
	return result, found
}

func parseJet4FormWidth(data []byte) int {
	if len(data) < 17 || le16(data) > 0x0014 {
		return 0
	}
	limit := len(data)
	if limit > 96 {
		limit = 96
	}
	// 窗体头的紧凑固定属性通常以 0x61/0x62/0x63 三项结束，0x63 是设计宽度。
	for pos := 8; pos+9 <= limit; pos++ {
		if data[pos] == 0x61 && data[pos+3] == 0x62 && data[pos+6] == 0x63 {
			width := int(le16(data[pos+7:]))
			if width > 0 && width <= 32767 {
				return width
			}
		}
	}
	return 0
}

type jet4TaggedTextField struct {
	Tag   byte
	Value string
}

func newTextFormProperty(id uint16, value string) FormProperty {
	return FormProperty{
		ID:        id,
		Name:      FormPropertyIDToName(id),
		ValueType: "Text",
		Value:     value,
	}
}

// parseJet4TaggedTextFields 读取一个控件块开头的紧凑文本项。
// 每项为 1 字节标签、1 字节 UTF-16LE 字节长度和文本；遇到 GUID 或数值区即停止。
func parseJet4TaggedTextFields(block []byte, controlName string) []jet4TaggedTextField {
	return parseJet4TaggedTextFieldsForType(block, controlName, "")
}

func parseJet4TaggedTextFieldsForType(block []byte, controlName, controlType string) []jet4TaggedTextField {
	nameBytes := encodeUTF16LE(controlName)
	if len(nameBytes) == 0 || !hasUTF16LEPrefixFoldASCII(block, nameBytes) {
		return nil
	}

	fields := make([]jet4TaggedTextField, 0, 5)
	for pos := len(nameBytes); pos+4 <= len(block); {
		tag := block[pos]
		byteLen := int(block[pos+1])
		if tag < 0xC0 || byteLen < 2 || byteLen%2 != 0 || pos+2+byteLen > len(block) {
			break
		}
		// 这些标签后保存 16 字节对象 GUID。GUID 偶尔能被误解码成可打印的
		// CJK 字符；若把它当成文本，会使另一个控件的同名 Tag 被误认为块起点。
		if byteLen == 16 && isJet4ControlGUIDTagForType(tag, controlType) {
			break
		}
		value, ok := decodeJet4UTF16Text(block[pos+2 : pos+2+byteLen])
		if !ok {
			break
		}
		fields = append(fields, jet4TaggedTextField{Tag: tag, Value: value})
		pos += 2 + byteLen
	}
	return fields
}

func isJet4ControlGUIDTagForType(tag byte, controlType string) bool {
	switch controlType {
	case "Label":
		return tag == 0xEA
	case "CheckBox":
		return tag == 0xF4
	case "OptionGroup", "OptionButton":
		return tag == 0xF4
	}
	return isJet4ControlGUIDTag(tag)
}

func isJet4ControlGUIDTag(tag byte) bool {
	switch tag {
	case 0xE4, 0xE5, 0xE7, 0xEA, 0xEB, 0xF5, 0xFA:
		return true
	default:
		return false
	}
}

func scanJet4TaggedTextFields(data []byte) []jet4TaggedTextField {
	fields := make([]jet4TaggedTextField, 0)
	for pos := 0; pos+4 <= len(data); pos++ {
		tag := data[pos]
		byteLen := int(data[pos+1])
		if tag < 0xC0 || byteLen < 2 || byteLen%2 != 0 || pos+2+byteLen > len(data) {
			continue
		}
		value, ok := decodeJet4UTF16Text(data[pos+2 : pos+2+byteLen])
		if !ok {
			continue
		}
		fields = append(fields, jet4TaggedTextField{Tag: tag, Value: value})
		pos += 1 + byteLen
	}
	return fields
}

func decodeJet4UTF16Text(data []byte) (string, bool) {
	if len(data) < 2 || len(data)%2 != 0 {
		return "", false
	}
	values := make([]uint16, 0, len(data)/2)
	for pos := 0; pos < len(data); pos += 2 {
		value := le16(data[pos:])
		if value == 0 {
			return "", false
		}
		r := rune(value)
		if !unicode.IsPrint(r) && r != '\t' && r != '\r' && r != '\n' {
			return "", false
		}
		values = append(values, value)
	}
	value := string(utf16.Decode(values))
	return value, strings.TrimSpace(value) != ""
}

func encodeUTF16LE(value string) []byte {
	encoded := utf16.Encode([]rune(value))
	result := make([]byte, len(encoded)*2)
	for i, item := range encoded {
		binary.LittleEndian.PutUint16(result[i*2:], item)
	}
	return result
}

func orderedFormControlOffsets(data []byte, controls []FormControlInfo) []int {
	offsets := make([]int, len(controls))
	for i := range offsets {
		offsets[i] = -1
	}

	// TypeInfo 按逻辑层级列出节和控件，而 Blob 中的节标记可能在子控件之后。
	// 因此先为每个控件独立寻找能通过长度标记文本校验的物理块，不能强制全局单调。
	firstStructuredOffset := len(data)
	for i, control := range controls {
		nameBytes := encodeUTF16LE(control.Name)
		bestOffset := -1
		bestScore := 0
		for _, off := range findUTF16LETokenOffsets(data, control.Name) {
			if !hasJet4ControlNameBoundary(data, off, len(nameBytes)) {
				continue
			}
			fields := parseJet4TaggedTextFieldsForType(data[off:], control.Name, control.Type)
			score := jet4TaggedTextFieldsControlTypeScore(control.Type, fields)
			if score > bestScore {
				bestOffset = off
				bestScore = score
			}
		}
		if bestOffset >= 0 {
			offsets[i] = bestOffset
			if bestOffset < firstStructuredOffset {
				firstStructuredOffset = bestOffset
			}
		}
	}

	// 节、矩形等对象可能没有文本项；只在已确认的设计区附近补充它们的起点，
	// 避免误用 Blob 前部名称字典中的同名字符串。
	designStart := 0
	if firstStructuredOffset < len(data) && firstStructuredOffset > 512 {
		designStart = firstStructuredOffset - 512
	}
	for i, control := range controls {
		if offsets[i] >= 0 {
			continue
		}
		nameBytes := encodeUTF16LE(control.Name)
		for _, off := range findUTF16LETokenOffsets(data, control.Name) {
			if off >= designStart && hasJet4ControlNameBoundary(data, off, len(nameBytes)) {
				offsets[i] = off
				break
			}
		}
	}

	// 名称还可能出现在 Blob 前部的窗体属性/字段字典中。正常控件块不会位于
	// 第一个 Section 物理标记之前；若初次命中落在该边界前，改取设计区内
	// 能通过 tagged-text 校验的同名块。
	firstSectionOffset := len(data)
	for i, control := range controls {
		if isFormSectionTypeCode(control.TypeCode) && offsets[i] >= 0 && offsets[i] < firstSectionOffset {
			firstSectionOffset = offsets[i]
		}
	}
	if firstSectionOffset < len(data) {
		for i, control := range controls {
			if isFormSectionTypeCode(control.TypeCode) || offsets[i] < 0 || offsets[i] >= firstSectionOffset {
				continue
			}
			bestOffset := -1
			bestScore := 0
			for _, off := range findUTF16LETokenOffsets(data, control.Name) {
				if off < firstSectionOffset ||
					!hasJet4ControlNameBoundary(data, off, len(encodeUTF16LE(control.Name))) {
					continue
				}
				fields := parseJet4TaggedTextFieldsForType(data[off:], control.Name, control.Type)
				score := jet4TaggedTextFieldsControlTypeScore(control.Type, fields)
				if score > bestScore {
					bestOffset = off
					bestScore = score
				}
			}
			if bestOffset >= 0 {
				offsets[i] = bestOffset
			}
		}
	}
	return offsets
}

func jet4TaggedTextFieldsMatchControlType(controlType string, fields []jet4TaggedTextField) bool {
	return jet4TaggedTextFieldsControlTypeScore(controlType, fields) > 0
}

func jet4TaggedTextFieldsControlTypeScore(controlType string, fields []jet4TaggedTextField) int {
	if len(fields) == 0 {
		return 0
	}
	score := 0
	for _, field := range fields {
		switch controlType {
		case "TextBox":
			// TextBox 名称可能与 Label.Caption 相同。真正的 TextBox 块至少
			// 包含 ControlSource、Format、StatusBarText、Tag 中的一项。
			switch field.Tag {
			case 0xDD:
				score += 6
			case 0xDF, 0xF4:
				score += 3
			case 0xDE:
				if !isKnownFormFont(field.Value) {
					score += 4
				}
			}
		case "ComboBox":
			// ComboBox 的短名称（例如 mid）经常出现在 Label.Caption 中。
			// 仅有 DE=FontName 的候选不是 ComboBox 块。
			switch field.Tag {
			case 0xDD, 0xDF:
				score += 6
			case 0xDE:
				if !isKnownFormFont(field.Value) {
					score += 4
				}
			case 0xE0:
				score += 5
			case 0xEA:
				score++
			case 0xF5:
				score += 3
			}
		case "TabPage":
			// Page.Name 也可能出现在普通文本中；Caption/EventProcPrefix
			// 才能确认真实 Page 块。
			switch field.Tag {
			case 0xE3, 0xE8:
				score += 6
			case 0xEA:
				score++
			}
		case "SubForm":
			switch field.Tag {
			case 0xDD:
				score += 6
			case 0xDE, 0xDF, 0xE0, 0xE3:
				score += 3
			}
		case "Label", "Button", "CheckBox", "OptionGroup", "OptionButton", "TabControl":
			// 这些类型仍允许只有字体/Caption 的紧凑块，但优先选择带有
			// 首项文本属性的完整候选，避免命中另一个控件的文本片段。
			if field.Tag == 0xDD {
				score += 4
			} else {
				score++
			}
		default:
			score++
		}
	}
	return score
}

func jet4ControlNameAt(data []byte, offset int, fallback string) string {
	nameBytes := encodeUTF16LE(fallback)
	if offset < 0 || len(nameBytes) == 0 || offset+len(nameBytes) > len(data) {
		return fallback
	}
	name, ok := decodeJet4UTF16Text(data[offset : offset+len(nameBytes)])
	if !ok || !strings.EqualFold(name, fallback) {
		return fallback
	}
	return name
}

func hasJet4ControlNameBoundary(data []byte, offset, byteLen int) bool {
	if byteLen <= 0 || offset < 0 || offset+byteLen > len(data) {
		return false
	}
	// 控件名可能紧跟任意二进制记录，不能把前一个 uint16 普遍解释成
	// Unicode 标识符。排除已知的名称片段误命中，例如在 lbl_house_no
	// 中命中 house_no，或在 entry_seq_code1 中命中 entry_seq_code。
	if offset >= 2 && le16(data[offset-2:]) == '_' {
		return false
	}
	if offset+byteLen+2 <= len(data) {
		next := rune(le16(data[offset+byteLen:]))
		if next == '_' || next >= '0' && next <= '9' ||
			next >= 'A' && next <= 'Z' || next >= 'a' && next <= 'z' {
			return false
		}
	}
	return true
}

func normalizeRecordSource(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || isKnownFormFont(value) {
		return "", false
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "select ") || strings.HasPrefix(value, "[") {
		return value, true
	}
	if !strings.HasPrefix(lower, "v_") && !strings.HasPrefix(lower, "t_") &&
		!strings.HasPrefix(lower, "q_") {
		return "", false
	}
	return normalizeASCIIFormIdentifier(value)
}

func normalizeTaggedRecordSource(value string) (string, bool) {
	if source, ok := normalizeRecordSource(value); ok {
		return source, true
	}
	value = strings.TrimSpace(value)
	if value == "" || isKnownFormFont(value) {
		return "", false
	}
	hasLetterOrDigit := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasLetterOrDigit = true
			continue
		}
		switch r {
		case '_', '.', '!', '[', ']', '-', ' ':
			continue
		default:
			return "", false
		}
	}
	return value, hasLetterOrDigit
}

func normalizeControlSource(value, controlName string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || isKnownFormFont(value) {
		return "", false
	}
	if strings.HasPrefix(value, "=") || strings.HasPrefix(value, "[") {
		return value, true
	}

	// 英文标识符后若紧跟布局二进制被误解码出的 Unicode 字符，只保留合法 ASCII 前缀。
	if value[0] < utf8.RuneSelf {
		return normalizeASCIIFormIdentifier(value)
	}

	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '_', '.', '!', '[', ']':
			continue
		default:
			return "", false
		}
	}
	return value, true
}

func normalizeASCIIFormIdentifier(value string) (string, bool) {
	end := 0
	for i, r := range value {
		if r > unicode.MaxASCII {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			end = i + 1
			continue
		}
		switch r {
		case '_', '.', '!', '[', ']':
			end = i + 1
			continue
		default:
			return "", false
		}
	}
	if end > 0 {
		return value[:end], true
	}
	return "", false
}

func isKnownFormFont(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "arial", "calibri", "cambria", "courier new", "ms sans serif", "tahoma", "times new roman", "verdana":
		return true
	default:
		return false
	}
}

func mergeFormProperties(primary, fallback []FormProperty) []FormProperty {
	if len(fallback) == 0 {
		return primary
	}
	result := append([]FormProperty(nil), primary...)
	seen := make(map[uint16]struct{}, len(primary))
	for _, prop := range primary {
		seen[prop.ID] = struct{}{}
	}
	for _, prop := range fallback {
		if _, ok := seen[prop.ID]; ok {
			continue
		}
		result = append(result, prop)
		seen[prop.ID] = struct{}{}
	}
	return result
}

func parseFormBlobPropertyAt(data []byte, pos, end int) (FormProperty, int, bool) {
	if pos < 0 || end > len(data) || pos+14 > end {
		return FormProperty{}, pos, false
	}
	id := le16(data[pos:])
	typeCode := le32(data[pos+2:])
	byteLen := uint64(le32(data[pos+10:]))
	dataStart := pos + 14
	prop := FormProperty{ID: id, Name: FormPropertyIDToName(id)}

	hasBytes := func(n uint64) bool {
		return n <= uint64(end-dataStart)
	}
	switch typeCode {
	case 0x01:
		if !hasBytes(4) {
			return FormProperty{}, pos, false
		}
		prop.ValueType = "Bool"
		prop.Value = strconv.FormatBool(le32(data[dataStart:]) != 0)
		return prop, pos + 18, true
	case 0x02:
		if !hasBytes(5) {
			return FormProperty{}, pos, false
		}
		prop.ValueType = "Short"
		prop.Value = strconv.FormatInt(int64(int16(le16(data[dataStart:]))), 10)
		return prop, pos + 19, true
	case 0x03, 0x06:
		if !hasBytes(6) {
			return FormProperty{}, pos, false
		}
		prop.ValueType = "Long"
		prop.Value = strconv.FormatInt(int64(int32(le32(data[dataStart:]))), 10)
		return prop, pos + 20, true
	case 0x04:
		if !hasBytes(8) {
			return FormProperty{}, pos, false
		}
		prop.ValueType = "Color"
		prop.Value = fmt.Sprintf("#%06X", le32(data[dataStart:])&0x00FFFFFF)
		return prop, pos + 22, true
	case 0x08:
		if !hasBytes(12) {
			return FormProperty{}, pos, false
		}
		prop.ValueType = "Double"
		bits := uint64(le32(data[dataStart:])) | uint64(le32(data[dataStart+4:]))<<32
		prop.Value = strconv.FormatFloat(math.Float64frombits(bits), 'g', -1, 64)
		return prop, pos + 26, true
	case 0x09:
		if !hasBytes(20) {
			return FormProperty{}, pos, false
		}
		prop.ValueType = "Guid"
		prop.Value = formatFormGUID(data[dataStart : dataStart+16])
		return prop, pos + 34, true
	case 0x0A, 0x0C:
		if byteLen > uint64(end-dataStart) || byteLen+4 > uint64(end-dataStart) {
			return FormProperty{}, pos, false
		}
		n := int(byteLen)
		prop.ValueType = "Text"
		prop.Value = decodeUTF16LE(data[dataStart : dataStart+n])
		return prop, dataStart + n + 4, true
	case 0x0B:
		if byteLen > uint64(end-dataStart) || byteLen+4 > uint64(end-dataStart) {
			return FormProperty{}, pos, false
		}
		n := int(byteLen)
		prop.ValueType = "Binary"
		prop.Value = fmt.Sprintf("(%d bytes)", n)
		prop.RawValue = append([]byte(nil), data[dataStart:dataStart+n]...)
		return prop, dataStart + n + 4, true
	default:
		return FormProperty{}, pos, false
	}
}

func formatFormGUID(data []byte) string {
	if len(data) < 16 {
		return ""
	}
	return fmt.Sprintf("{%08X-%04X-%04X-%02X%02X-%02X%02X%02X%02X%02X%02X}",
		le32(data), le16(data[4:]), le16(data[6:]), data[8], data[9],
		data[10], data[11], data[12], data[13], data[14], data[15])
}

func formPropertyText(props []FormProperty, id uint16) string {
	for _, prop := range props {
		if prop.ID == id && prop.ValueType == "Text" {
			return prop.Value
		}
	}
	return ""
}

func formPropertyTextAny(props []FormProperty, ids ...uint16) string {
	for _, id := range ids {
		if value := formPropertyText(props, id); value != "" {
			return value
		}
	}
	return ""
}

func findFormPropertyGroupByName(groups [][]FormProperty, name string) int {
	for i, group := range groups {
		if strings.EqualFold(formPropertyText(group, 0x0014), name) {
			return i
		}
	}
	return -1
}

func inferControlTypeByName(name string) string {
	l := strings.ToLower(name)
	switch {
	case strings.HasPrefix(l, "label"), strings.HasPrefix(l, "lbl_"):
		return "Label"
	case strings.HasPrefix(l, "box"):
		return "Rectangle"
	case strings.HasPrefix(l, "btn"):
		return "Button"
	case strings.Contains(l, "check"), strings.HasSuffix(l, "_sw"):
		return "CheckBox"
	default:
		return "TextBox"
	}
}

func findUTF16LETokenOffsets(data []byte, token string) []int {
	if len(data) < 2 || token == "" {
		return nil
	}
	pat := encodeUTF16LE(token)

	out := make([]int, 0)
	for off := 0; off+len(pat) <= len(data); off++ {
		if hasUTF16LEPrefixFoldASCII(data[off:], pat) {
			out = append(out, off)
		}
	}
	return out
}

// hasUTF16LEPrefixFoldASCII 按 Access 名称规则比较 UTF-16LE 前缀。
// Access 控件名不区分 ASCII 大小写，但非 ASCII 字符仍保持原始码元精确比较。
func hasUTF16LEPrefixFoldASCII(data, pattern []byte) bool {
	if len(pattern) == 0 || len(pattern)%2 != 0 || len(data) < len(pattern) {
		return false
	}
	for pos := 0; pos < len(pattern); pos += 2 {
		actual := le16(data[pos:])
		expected := le16(pattern[pos:])
		if actual >= 'A' && actual <= 'Z' {
			actual += 'a' - 'A'
		}
		if expected >= 'A' && expected <= 'Z' {
			expected += 'a' - 'A'
		}
		if actual != expected {
			return false
		}
	}
	return true
}
