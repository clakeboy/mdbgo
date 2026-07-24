package mdbgo

import (
	"sort"
	"strconv"
	"strings"
)

func parseJet4LabelTextProperties(control FormControlInfo, fields []jet4TaggedTextField) []FormProperty {
	var props []FormProperty
	for _, field := range fields {
		switch field.Tag {
		case 0xDD:
			value := field.Value
			trimmed := strings.TrimSpace(value)
			if trimmed != "" && !strings.EqualFold(trimmed, control.Name) && !isKnownFormFont(trimmed) {
				props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0011, value)})
			}
		case 0xDE:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0022, field.Value)})
		case 0xE4:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x010A, field.Value)})
		}
	}
	return props
}

type jet4LabelNumericProperties struct {
	TextAlign      byte
	FontSize       int
	BackStyle      byte
	BackColor      string
	BackColorValue uint32
	ForeColor      string
	ForeColorValue uint32
	Geometry       formControlGeometry
	HasGeometry    bool
	hasBackColor   bool
	hasForeColor   bool
}

func (props jet4LabelNumericProperties) formProperties() []FormProperty {
	return []FormProperty{
		{ID: 0x001D, Name: FormPropertyIDToName(0x001D), ValueType: "Byte", Value: strconv.Itoa(int(props.BackStyle))},
		{ID: 0x0023, Name: FormPropertyIDToName(0x0023), ValueType: "Short", Value: strconv.Itoa(props.FontSize)},
		{ID: 0x0088, Name: FormPropertyIDToName(0x0088), ValueType: "Byte", Value: strconv.Itoa(int(props.TextAlign))},
		{ID: 0x001C, Name: FormPropertyIDToName(0x001C), ValueType: "Color", Value: props.BackColor},
		{ID: 0x00CC, Name: FormPropertyIDToName(0x00CC), ValueType: "Color", Value: props.ForeColor},
	}
}

// parseJet4FormLabelProperties 把 Label 紧凑数值记录按物理顺序与 Label 文本块配对。
// Label 的颜色组合标记是 0x9C (BackColor) 和 0x9E (ForeColor)。
func parseJet4FormLabelProperties(data []byte, controls []FormControlInfo) map[string]jet4LabelNumericProperties {
	result := make(map[string]jet4LabelNumericProperties)
	if len(data) < 8 || len(controls) == 0 || le16(data) > 0x0014 {
		return result
	}

	offsets := orderedFormControlOffsets(data, controls)
	type physicalLabel struct {
		offset int
		name   string
	}
	labels := make([]physicalLabel, 0)
	for i, offset := range offsets {
		if offset >= 0 && controls[i].Type == "Label" {
			labels = append(labels, physicalLabel{offset: offset, name: controls[i].Name})
		}
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i].offset < labels[j].offset })

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

	numericByLabel := make(map[int]jet4LabelNumericProperties, len(labels))
	for _, block := range blocks {
		tail := jet4ControlNumericTailForType(block.block, block.name, block.controlType)
		props, ok := parseJet4LabelNumericTail(tail)
		if !ok {
			continue
		}
		target := sort.Search(len(labels), func(i int) bool {
			return labels[i].offset > block.offset
		})
		if target < len(labels) {
			if _, exists := numericByLabel[target]; !exists {
				numericByLabel[target] = props
			}
		}
	}
	applyJet4LabelColorDefaults(numericByLabel)
	for i, props := range numericByLabel {
		result[strings.ToLower(labels[i].name)] = props
	}
	return result
}

// applyJet4LabelColorDefaults 还原窗体保存时采用的 Label 控件模板。
// Jet4 只写与模板不同的颜色：旧式 RGB 模板是白底黑字，系统模板是
// ButtonFace/ButtonText。出现“只写 BackColor”的记录时，其省略的
// ForeColor 即黑色，也能据此识别同一窗体内完全省略颜色的 Label。
func applyJet4LabelColorDefaults(records map[int]jet4LabelNumericProperties) {
	usesRGBDefaultColors := false
	for _, props := range records {
		// RGB 模板的默认背景是白色；需要 ButtonFace 时会单独写 0x9C，
		// 同时继续省略默认黑色 ForeColor。系统模板本身默认 ButtonFace，
		// 不会产生这种“只写默认系统背景”的记录。
		if props.hasBackColor && !props.hasForeColor && props.BackColorValue == 0x8000000F {
			usesRGBDefaultColors = true
			break
		}
	}
	if !usesRGBDefaultColors {
		return
	}
	for index, props := range records {
		if !props.hasBackColor {
			props.BackColorValue = 0x00FFFFFF
			props.BackColor = accessColorHex(props.BackColorValue)
		}
		if !props.hasForeColor {
			props.ForeColorValue = 0
			props.ForeColor = accessColorHex(props.ForeColorValue)
		}
		records[index] = props
	}
}

func parseJet4LabelNumericTail(tail []byte) (jet4LabelNumericProperties, bool) {
	result := jet4LabelNumericProperties{
		FontSize:       8,
		BackColor:      accessColorHex(0x8000000F),
		BackColorValue: 0x8000000F,
		// 系统控件模板省略颜色时使用 ButtonFace/ButtonText；窗体若采用
		// 白底黑字模板，会在整组记录解析完成后由 applyJet4LabelColorDefaults 覆盖。
		ForeColor:      accessColorHex(0x80000012),
		ForeColorValue: 0x80000012,
	}
	if len(tail) < 12 {
		return result, false
	}

	// Label 的紧凑记录类型为 0x0064。ForeColor 前可能插入
	// 0x9D BorderColor，因此不能依赖 0x9C/0x9E 必须相邻。
	recordPos := -1
	for pos := 0; pos+2 <= len(tail) && pos < 8; pos++ {
		if tail[pos] == 0x64 && tail[pos+1] == 0x00 {
			recordPos = pos + 2
			break
		}
	}
	if recordPos < 0 {
		return result, false
	}

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
	// Datasheet/continuous-form Label records store BackStyle as a compact
	// leading byte instead of the usual tagged 0x32 value.
	prefixPos := recordPos
	if prefixPos < layoutPos && tail[prefixPos] <= 1 {
		result.BackStyle = tail[prefixPos]
		prefixPos++
	}
	for pos := prefixPos; pos < layoutPos; {
		if tail[pos] < 0x30 || pos+1 >= layoutPos {
			pos++
			continue
		}
		tag, value := tail[pos], tail[pos+1]
		switch tag {
		case 0x32, 0x43:
			if value <= 1 {
				result.BackStyle = value
			}
		case 0x37, 0x3B:
			if value <= 4 {
				result.TextAlign = value
			}
		}
		pos += 2
	}

	result.Geometry.Width = 1440
	result.Geometry.Height = 288
	result.HasGeometry = true
	foundBackColor := false
	foundForeColor := false
	for pos := layoutPos; pos+2 < len(tail); {
		tag := tail[pos]
		switch tag {
		case 0x60, 0x61, 0x62, 0x63, 0x64:
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
			case 0x64:
				result.FontSize = value
			}
			pos += 3
		case 0x9C, 0x9D, 0x9E:
			if pos+5 > len(tail) {
				return result, false
			}
			value := le32(tail[pos+1:])
			if tag == 0x9C {
				result.BackColorValue = value
				result.BackColor = accessColorHex(value)
				foundBackColor = true
			} else {
				// Jet4 不同 Label 记录变体分别使用 0x9D 或 0x9E 保存 ForeColor。
				result.ForeColorValue = value
				result.ForeColor = accessColorHex(value)
				foundForeColor = true
			}
			pos += 5
		default:
			pos++
		}
	}
	result.hasBackColor = foundBackColor
	result.hasForeColor = foundForeColor
	// 未编码 BackColor 且显式使用系统 ForeColor 时，Access 保存的原生组合
	// 使用白色背景；两项都省略时则保留上面的系统默认组合。
	if !foundBackColor && foundForeColor && result.ForeColorValue&0x80000000 != 0 {
		result.BackColorValue = 0x00FFFFFF
		result.BackColor = accessColorHex(result.BackColorValue)
	}
	if result.Geometry.Width <= 0 || result.Geometry.Height <= 0 ||
		result.Geometry.Left > 32767 || result.Geometry.Top > 32767 ||
		result.Geometry.Width > 32767 || result.Geometry.Height > 32767 {
		return result, false
	}
	return result, true
}
