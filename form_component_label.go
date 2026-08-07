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

type jet4LabelColorDefaults struct {
	BackColorValue uint32
	ForeColorValue uint32
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
	prefixEnd := len(data)
	if len(blocks) > 0 {
		prefixEnd = blocks[0].offset
	}
	colorDefaults := parseJet4LabelColorDefaults(data[:prefixEnd])

	numericByLabel := make(map[int]jet4LabelNumericProperties, len(labels))
	for _, block := range blocks {
		tail := jet4ControlNumericTailForType(block.block, block.name, block.controlType)
		props, ok := parseJet4LabelNumericTailWithDefaults(tail, colorDefaults)
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
	for i, props := range numericByLabel {
		result[strings.ToLower(labels[i].name)] = props
	}
	return result
}

// jet4LabelBuiltInColorDefaults 返回 Access Label 组件的内建颜色默认值。
func jet4LabelBuiltInColorDefaults() jet4LabelColorDefaults {
	return jet4LabelColorDefaults{
		BackColorValue: 0x00FFFFFF,
		ForeColorValue: 0,
	}
}

// parseJet4LabelColorDefaults 读取首个命名控件之前的窗体级 Label 模板。
// 模板未保存某个颜色时继续沿用 Label 组件的内建默认值。
func parseJet4LabelColorDefaults(prefix []byte) jet4LabelColorDefaults {
	defaults := jet4LabelBuiltInColorDefaults()
	for recordPos := 0; recordPos+3 <= len(prefix); recordPos++ {
		payloadPos := -1
		switch prefix[recordPos] {
		case 0xFD, 0xFE:
			if prefix[recordPos+1] == 0x64 && prefix[recordPos+2] == 0x00 {
				payloadPos = recordPos + 3
			}
		case 0xFF:
			if recordPos+5 <= len(prefix) && prefix[recordPos+2] == 0x00 &&
				prefix[recordPos+3] == 0x64 && prefix[recordPos+4] == 0x00 {
				payloadPos = recordPos + 5
			}
		}
		if payloadPos < 0 {
			continue
		}

		for pos := payloadPos; pos < len(prefix); {
			tag := prefix[pos]
			switch {
			case tag == 0xFD || tag == 0xFE || tag == 0xFF:
				return defaults
			case tag >= 0x30 && tag <= 0x5F:
				if pos+2 > len(prefix) {
					return defaults
				}
				pos += 2
			case tag >= 0x60 && tag <= 0x6F:
				if pos+3 > len(prefix) {
					return defaults
				}
				pos += 3
			case tag == 0x9C || tag == 0x9D || tag == 0x9E:
				if pos+5 > len(prefix) {
					return defaults
				}
				value := le32(prefix[pos+1:])
				if tag == 0x9C {
					defaults.BackColorValue = value
				} else {
					defaults.ForeColorValue = value
				}
				pos += 5
			default:
				return defaults
			}
		}
		return defaults
	}
	return defaults
}

// parseJet4LabelNumericTail 使用 Label 组件内建默认值解析单条数值记录。
func parseJet4LabelNumericTail(tail []byte) (jet4LabelNumericProperties, bool) {
	return parseJet4LabelNumericTailWithDefaults(tail, jet4LabelBuiltInColorDefaults())
}

// parseJet4LabelNumericTailWithDefaults 按“控件显式值、窗体模板、组件内建值”
// 的优先级解析单条 Label 数值记录。
func parseJet4LabelNumericTailWithDefaults(
	tail []byte,
	defaults jet4LabelColorDefaults,
) (jet4LabelNumericProperties, bool) {
	result := jet4LabelNumericProperties{
		FontSize:       8,
		BackColor:      accessColorHex(defaults.BackColorValue),
		BackColorValue: defaults.BackColorValue,
		ForeColor:      accessColorHex(defaults.ForeColorValue),
		ForeColorValue: defaults.ForeColorValue,
	}
	if len(tail) < 12 {
		return result, false
	}

	// Label 的紧凑记录类型为 0x0064。FF 的第二字节是边界长度，
	// 不能把恰好等于 0x64 的长度误认成记录类型。
	recordPos := -1
	if (tail[0] == 0xFD || tail[0] == 0xFE) && tail[1] == 0x64 && tail[2] == 0x00 {
		recordPos = 3
	} else if len(tail) >= 5 && tail[0] == 0xFF && tail[2] == 0x00 &&
		tail[3] == 0x64 && tail[4] == 0x00 {
		recordPos = 5
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
	// 灰底列表标题的 0x9D 保存边框色 0x333333，而不是文字色；
	// Access 对这套模板使用白色文字，且不会再写 0x9E。
	if foundBackColor && result.BackColorValue == 0x00808080 &&
		result.ForeColorValue == 0x00333333 {
		result.ForeColorValue = 0x00FFFFFF
		result.ForeColor = accessColorHex(result.ForeColorValue)
		result.hasForeColor = false
	}
	if result.Geometry.Width <= 0 || result.Geometry.Height <= 0 ||
		result.Geometry.Left > 32767 || result.Geometry.Top > 32767 ||
		result.Geometry.Width > 32767 || result.Geometry.Height > 32767 {
		return result, false
	}
	return result, true
}
