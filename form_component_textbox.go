package mdbgo

import (
	"sort"
	"strconv"
	"strings"
)

func parseJet4TextBoxTextProperties(control FormControlInfo, fields []jet4TaggedTextField) []FormProperty {
	var props []FormProperty
	for _, field := range fields {
		switch field.Tag {
		case 0xDD:
			if source, ok := normalizeControlSource(field.Value, control.Name); ok {
				props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x001B, source)})
			}
		case 0xDE:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0026, field.Value)})
		case 0xDF:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0087, field.Value)})
		case 0xE8:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0022, field.Value)})
		case 0xF4:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x010A, field.Value)})
		}
	}
	return props
}

type jet4FormNumericProperties struct {
	TextAlign       byte
	TabIndex        int
	HasTabIndex     bool
	ScrollBars      byte
	Locked          bool
	Underline       bool
	BackStyle       byte
	BackColor       string
	BackColorValue  uint32
	ForeColor       string
	ForeColorValue  uint32
	Geometry        formControlGeometry
	HasGeometry     bool
	hasForeColor    bool
	usesRGBDefaults bool
}

func (props jet4FormNumericProperties) formProperties() []FormProperty {
	return []FormProperty{
		{ID: 0x0038, Name: FormPropertyIDToName(0x0038), ValueType: "Bool", Value: strconv.FormatBool(props.Locked)},
		{ID: 0x0024, Name: FormPropertyIDToName(0x0024), ValueType: "Bool", Value: strconv.FormatBool(props.Underline)},
		{ID: 0x001D, Name: FormPropertyIDToName(0x001D), ValueType: "Byte", Value: strconv.Itoa(int(props.BackStyle))},
		{ID: 0x0088, Name: FormPropertyIDToName(0x0088), ValueType: "Byte", Value: strconv.Itoa(int(props.TextAlign))},
		{ID: 0x0105, Name: FormPropertyIDToName(0x0105), ValueType: "Short", Value: strconv.Itoa(props.TabIndex)},
		{ID: 0x0098, Name: FormPropertyIDToName(0x0098), ValueType: "Byte", Value: strconv.Itoa(int(props.ScrollBars))},
		{ID: 0x001C, Name: FormPropertyIDToName(0x001C), ValueType: "Color", Value: props.BackColor},
		{ID: 0x00CC, Name: FormPropertyIDToName(0x00CC), ValueType: "Color", Value: props.ForeColor},
	}
}

// parseJet4FormNumericProperties 解析 TextBox 的紧凑数值属性。
// Jet4 Blob 中文本目录和紧凑数值记录是两套顺序：0x006D 数值记录保存在
// 前一个物理控件块尾部，属于它后面的第一个 TextBox。按物理邻接绑定可跳过
// 没有布局记录的隐藏 TextBox，避免其后全部属性错位。
func parseJet4FormNumericProperties(data []byte, controls []FormControlInfo) map[string]jet4FormNumericProperties {
	result := make(map[string]jet4FormNumericProperties)
	if len(data) < 8 || len(controls) == 0 || le16(data) > 0x0014 {
		return result
	}

	offsets := orderedFormControlOffsets(data, controls)
	type physicalTextBox struct {
		offset int
		name   string
	}
	textBoxes := make([]physicalTextBox, 0)
	for i, offset := range offsets {
		if offset < 0 || controls[i].Type != "TextBox" {
			continue
		}
		textBoxes = append(textBoxes, physicalTextBox{offset: offset, name: controls[i].Name})
	}
	sort.Slice(textBoxes, func(i, j int) bool { return textBoxes[i].offset < textBoxes[j].offset })

	type physicalControlBlock struct {
		offset      int
		name        string
		controlType string
		block       []byte
	}
	blocks := make([]physicalControlBlock, 0, len(controls))
	seenOffsets := make(map[int]bool, len(offsets))
	for i, offset := range offsets {
		if offset < 0 || seenOffsets[offset] {
			continue
		}
		seenOffsets[offset] = true
		block := jet4FormControlBlock(data, offsets, i)
		if len(block) == 0 {
			continue
		}
		blocks = append(blocks, physicalControlBlock{
			offset: offset, name: controls[i].Name, controlType: controls[i].Type, block: block,
		})
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].offset < blocks[j].offset })
	numericByTextBox := make(map[int]jet4FormNumericProperties, len(textBoxes))
	for _, block := range blocks {
		tail := jet4ControlNumericTailForType(block.block, block.name, block.controlType)
		props, ok := parseJet4TextBoxNumericTail(tail)
		if !ok {
			continue
		}
		target := sort.Search(len(textBoxes), func(i int) bool {
			return textBoxes[i].offset > block.offset
		})
		if target < len(textBoxes) {
			if _, exists := numericByTextBox[target]; !exists {
				numericByTextBox[target] = props
			}
		}
	}
	applyJet4TextBoxColorDefaults(numericByTextBox)

	hasTabPages := false
	for _, control := range controls {
		if control.Type == "TabPage" {
			hasTabPages = true
			break
		}
	}
	previousTabIndex := -1
	previousOffset := -1
	for i := 0; i < len(textBoxes); i++ {
		props, ok := numericByTextBox[i]
		if !ok {
			continue
		}
		if hasTabPages && previousTabIndex >= 0 {
			if !props.HasTabIndex {
				if hasJet4ControlTypeBetween(controls, offsets, previousOffset, textBoxes[i].offset, "TabPage") {
					props.TabIndex = 0
				} else {
					props.TabIndex = previousTabIndex + 1
				}
			} else if props.TabIndex > previousTabIndex+1 &&
				!hasJet4FocusableControlBetween(controls, offsets, previousOffset, textBoxes[i].offset) {
				props.TabIndex = previousTabIndex + 1
			}
		}
		result[strings.ToLower(textBoxes[i].name)] = props
		previousTabIndex = props.TabIndex
		previousOffset = textBoxes[i].offset
	}
	return result
}

// applyJet4TextBoxColorDefaults 还原窗体级 TextBox 模板省略的 ForeColor。
// Datasheet 的 0x30 项或 RGB 模板的 0x35 标志只需在部分记录中出现，
// 同一窗体里其余未显式写 0x9F 的 TextBox 也使用默认黑色。
func applyJet4TextBoxColorDefaults(records map[int]jet4FormNumericProperties) {
	usesRGBDefaults := false
	for _, props := range records {
		if props.usesRGBDefaults {
			usesRGBDefaults = true
			break
		}
	}
	if !usesRGBDefaults {
		return
	}
	for index, props := range records {
		if !props.hasForeColor {
			props.ForeColorValue = 0
			props.ForeColor = accessColorHex(props.ForeColorValue)
			records[index] = props
		}
	}
}

func hasJet4ControlTypeBetween(controls []FormControlInfo, offsets []int, start, end int, controlType string) bool {
	for i, offset := range offsets {
		if offset > start && offset < end && controls[i].Type == controlType {
			return true
		}
	}
	return false
}

func hasJet4FocusableControlBetween(controls []FormControlInfo, offsets []int, start, end int) bool {
	for i, offset := range offsets {
		if offset <= start || offset >= end {
			continue
		}
		switch controls[i].Type {
		case "Button", "ToggleButton", "CheckBox", "OptionButton", "ComboBox", "ListBox", "SubForm":
			return true
		}
	}
	return false
}

func parseJet4TextBoxNumericTail(tail []byte) (jet4FormNumericProperties, bool) {
	result := jet4FormNumericProperties{
		BackStyle:      1,
		BackColor:      accessColorHex(0x00FFFFFF),
		BackColorValue: 0x00FFFFFF,
		// Access TextBox 未显式保存 ForeColor 时使用系统 WindowText。
		// 显式的普通 RGB 颜色仍会由 0x9F 覆盖。
		ForeColor:      accessColorHex(0x80000008),
		ForeColorValue: 0x80000008,
	}
	if len(tail) < 12 {
		return result, false
	}

	// TextBox 的紧凑记录类型为 0x006D。记录有时直接以 FD 6D 00
	// 开始，有时嵌在 FF <len> 00 6D 00 之后。
	recordPos := -1
	for pos := 0; pos+2 <= len(tail) && pos < 8; pos++ {
		if tail[pos] == 0x6D && tail[pos+1] == 0x00 {
			recordPos = pos + 2
			break
		}
	}
	if recordPos < 0 {
		return result, false
	}

	// Locked 是记录前缀里的独立布尔标志 0x02，不是带值的 tagged 项。
	// 首条记录可能先带一个 0x01 前缀，再保存 0x02。
	result.Locked = tail[recordPos] == 0x02 ||
		(recordPos+1 < len(tail) && tail[recordPos] == 0x01 && tail[recordPos+1] == 0x02)
	result.Underline = tail[recordPos] == 0x0A ||
		(recordPos+1 < len(tail) && tail[recordPos] == 0x02 && tail[recordPos+1] == 0x0A)

	layoutPos := -1
	for pos := recordPos; pos+2 < len(tail); pos++ {
		if tail[pos] >= 0x60 && tail[pos] <= 0x63 {
			layoutPos = pos
			break
		}
	}
	if layoutPos < 0 {
		return result, false
	}

	usesRGBDefaultColors := false
	for pos := recordPos; pos < layoutPos; {
		// 紧凑前缀中的 0x30..0x5F 项均为 tag/value 两字节；布尔标志
		// （如 Locked 的 0x02）为单字节。按项推进可避免把某个 value
		// 恰好等于 0x32 时误认成 ScrollBars 标签。
		if tail[pos] < 0x30 || pos+1 >= layoutPos {
			pos++
			continue
		}
		tag, value := tail[pos], tail[pos+1]
		switch tag {
		case 0x30:
			// Datasheet 模板的默认文字色同样是 RGB 黑色。
			usesRGBDefaultColors = true
		case 0x32:
			if value <= 3 {
				result.ScrollBars = value
			}
		case 0x35:
			// Jet4 的 RGB 控件模板把 ForeColor=0 作为默认值，因此不会再
			// 写 0x9F；0x35 的最低位用于标记这套模板。
			usesRGBDefaultColors = usesRGBDefaultColors || value&0x01 != 0
		case 0x3B:
			if value <= 4 {
				result.TextAlign = value
			}
		case 0x43:
			if value <= 1 {
				result.BackStyle = value
			}
		}
		pos += 2
	}

	// Access TextBox 省略与默认值相同的尺寸项。
	result.Geometry.Width = 1440
	result.Geometry.Height = 288
	result.HasGeometry = true
	foundForeColor := false
	for pos := layoutPos; pos+2 < len(tail); {
		tag := tail[pos]
		switch tag {
		case 0x60, 0x61, 0x62, 0x63, 0x69, 0x6B:
			rawValue := le16(tail[pos+1:])
			value := int(rawValue)
			switch tag {
			case 0x60:
				result.Geometry.Left = int(int16(rawValue))
			case 0x61:
				result.Geometry.Top = int(int16(rawValue))
			case 0x62:
				result.Geometry.Width = value
			case 0x63:
				result.Geometry.Height = value
			case 0x6B:
				result.TabIndex = value
				result.HasTabIndex = true
			}
			pos += 3
		case 0x9C, 0x9F:
			if pos+5 > len(tail) {
				return result, false
			}
			value := le32(tail[pos+1:])
			if tag == 0x9C {
				result.BackColorValue = value
				result.BackColor = accessColorHex(value)
			} else {
				result.ForeColorValue = value
				result.ForeColor = accessColorHex(value)
				foundForeColor = true
			}
			pos += 5
		case 0xDC:
			pos = len(tail)
		default:
			pos++
		}
	}
	if usesRGBDefaultColors && !foundForeColor {
		result.ForeColorValue = 0
		result.ForeColor = accessColorHex(0)
	}
	result.hasForeColor = foundForeColor
	result.usesRGBDefaults = usesRGBDefaultColors
	if result.Geometry.Width <= 0 || result.Geometry.Height <= 0 ||
		result.Geometry.Left > 32767 || result.Geometry.Top > 32767 ||
		result.Geometry.Width > 32767 || result.Geometry.Height > 32767 {
		return result, false
	}
	return result, true
}
