package mdbgo

import (
	"bytes"
	"sort"
	"strconv"
	"strings"
)

const jet4OptionButtonBuiltInDefaultHeight = 240

// parseJet4OptionButtonTextProperties 解析 OptionButton 的字符串属性。
// 0xDD/0xDE/0xF0 分别保存 ControlSource、StatusBarText 和 Tag。
func parseJet4OptionButtonTextProperties(control FormControlInfo, fields []jet4TaggedTextField) []FormProperty {
	var props []FormProperty
	for _, field := range fields {
		switch field.Tag {
		case 0xDD:
			if source, ok := normalizeControlSource(field.Value, control.Name); ok {
				props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x001B, source)})
			}
		case 0xDE:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0087, field.Value)})
		case 0xF0:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x010A, field.Value)})
		}
	}
	return props
}

type jet4OptionButtonNumericProperties struct {
	OptionValue      int
	HasOptionValue   bool
	SpecialEffect    byte
	HasSpecialEffect bool
	BorderStyle      byte
	HasBorderStyle   bool
	BorderWidth      byte
	HasBorderWidth   bool
	BorderColor      string
	BorderColorValue uint32
	HasBorderColor   bool
	TabIndex         int
	HasTabIndex      bool
	Locked           bool
	Visible          bool
	Geometry         formControlGeometry
	HasGeometry      bool
}

func (props jet4OptionButtonNumericProperties) formProperties() []FormProperty {
	result := []FormProperty{
		{ID: 0x003A, Name: FormPropertyIDToName(0x003A), ValueType: "Long", Value: strconv.Itoa(props.OptionValue)},
		{ID: 0x0038, Name: FormPropertyIDToName(0x0038), ValueType: "Bool", Value: strconv.FormatBool(props.Locked)},
		{ID: 0x0094, Name: FormPropertyIDToName(0x0094), ValueType: "Bool", Value: strconv.FormatBool(props.Visible)},
	}
	if props.HasSpecialEffect {
		result = append(result, FormProperty{
			ID: 0x0004, Name: FormPropertyIDToName(0x0004), ValueType: "Byte", Value: strconv.Itoa(int(props.SpecialEffect)),
		})
	}
	if props.HasBorderStyle {
		result = append(result, FormProperty{
			ID: 0x0009, Name: FormPropertyIDToName(0x0009), ValueType: "Byte", Value: strconv.Itoa(int(props.BorderStyle)),
		})
	}
	if props.HasBorderWidth {
		result = append(result, FormProperty{
			ID: 0x000A, Name: FormPropertyIDToName(0x000A), ValueType: "Byte", Value: strconv.Itoa(int(props.BorderWidth)),
		})
	}
	if props.HasBorderColor {
		result = append(result, FormProperty{
			ID: 0x0008, Name: FormPropertyIDToName(0x0008), ValueType: "Color", Value: props.BorderColor,
		})
	}
	if props.HasTabIndex {
		result = append(result, FormProperty{
			ID: 0x0105, Name: FormPropertyIDToName(0x0105), ValueType: "Short", Value: strconv.Itoa(props.TabIndex),
		})
	}
	return result
}

// parseJet4FormOptionButtonProperties 按物理顺序把 0x69 OptionButton 数值记录配回控件。
// 数值记录可能保存于相邻 OptionGroup 或 Label 的文本块尾部，因此先扫描所有物理块，
// 再按出现顺序与 OptionButton 的文本块配对。
func parseJet4FormOptionButtonProperties(data []byte, controls []FormControlInfo) map[string]jet4OptionButtonNumericProperties {
	result := make(map[string]jet4OptionButtonNumericProperties)
	if len(data) < 8 || len(controls) == 0 || le16(data) > 0x0014 {
		return result
	}

	offsets := orderedFormControlOffsets(data, controls)
	type physicalOptionButton struct {
		offset int
		name   string
	}
	optionButtons := make([]physicalOptionButton, 0)
	for i, offset := range offsets {
		if offset >= 0 && controls[i].Type == "OptionButton" {
			optionButtons = append(optionButtons, physicalOptionButton{offset: offset, name: controls[i].Name})
		}
	}
	sort.Slice(optionButtons, func(i, j int) bool { return optionButtons[i].offset < optionButtons[j].offset })

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
	defaultHeight := parseJet4OptionButtonDefaultHeight(data[:prefixEnd])
	numericRecords := make([]jet4OptionButtonNumericProperties, 0, len(optionButtons))
	for _, block := range blocks {
		tail := jet4ControlNumericTailForType(block.block, block.name, block.controlType)
		props, ok := parseJet4OptionButtonNumericTailWithDefaultHeight(tail, defaultHeight)
		if ok {
			numericRecords = append(numericRecords, props)
		}
	}
	for i := 0; i < len(optionButtons) && i < len(numericRecords); i++ {
		result[strings.ToLower(optionButtons[i].name)] = numericRecords[i]
	}
	return result
}

// parseJet4OptionButtonDefaultHeight 读取命名控件区之前的窗体级 OptionButton 模板。
// 模板以 FD 69 00 开始；若模板省略 0x63 Height，则使用 Access 内建的
// 240-twip 默认高度。
func parseJet4OptionButtonDefaultHeight(prefix []byte) int {
	signature := []byte{0xFD, 0x69, 0x00}
	for searchPos := 0; searchPos+len(signature) <= len(prefix); {
		relative := bytes.Index(prefix[searchPos:], signature)
		if relative < 0 {
			break
		}
		recordPos := searchPos + relative
		height := jet4OptionButtonBuiltInDefaultHeight
		isTemplate := false
		for pos := recordPos + len(signature); pos < len(prefix) && pos < recordPos+96; {
			tag := prefix[pos]
			switch tag {
			case 0x31, 0x32, 0x33, 0x34:
				if pos+2 > len(prefix) {
					pos = len(prefix)
					continue
				}
				if tag == 0x31 {
					isTemplate = true
				}
				pos += 2
			case 0x60, 0x61, 0x62, 0x63, 0x67, 0x68:
				if pos+3 > len(prefix) {
					pos = len(prefix)
					continue
				}
				if tag == 0x63 {
					value := int(le16(prefix[pos+1:]))
					if value > 0 && value <= 32767 {
						height = value
					}
				}
				pos += 3
			case 0x9C, 0x9D:
				if pos+5 > len(prefix) {
					pos = len(prefix)
					continue
				}
				pos += 5
			default:
				pos = len(prefix)
			}
		}
		if isTemplate {
			return height
		}
		searchPos = recordPos + len(signature)
	}
	return jet4OptionButtonBuiltInDefaultHeight
}

func parseJet4OptionButtonNumericTail(tail []byte) (jet4OptionButtonNumericProperties, bool) {
	return parseJet4OptionButtonNumericTailWithDefaultHeight(
		tail, jet4OptionButtonBuiltInDefaultHeight)
}

func parseJet4OptionButtonNumericTailWithDefaultHeight(
	tail []byte,
	defaultHeight int,
) (jet4OptionButtonNumericProperties, bool) {
	// Access 会省略默认高度；这种记录仍是完整的 OptionButton，不能因为
	// 没有 0x63 就丢弃并造成后续控件错位。具体默认值由文件格式调用方传入。
	result := jet4OptionButtonNumericProperties{
		Visible:  true,
		Geometry: formControlGeometry{Height: defaultHeight},
	}
	if len(tail) < 12 {
		return result, false
	}

	payloadPos := -1
	for pos := 0; pos+3 <= len(tail) && pos < 12; pos++ {
		if tail[pos] == 0xFD && tail[pos+1] == 0x69 && tail[pos+2] == 0x00 {
			payloadPos = pos + 3
			break
		}
	}
	// 第一个 OptionButton 可能紧邻 OptionGroup 边界；此时记录以 FF 掩码开头，
	// 03 00 后面的 69 00 才是 OptionButton 类型标记。
	if payloadPos < 0 && tail[0] == 0xFF {
		for pos := 1; pos+2 <= len(tail) && pos < 8; pos++ {
			if tail[pos] == 0x69 && tail[pos+1] == 0x00 {
				payloadPos = pos + 2
				break
			}
		}
	}
	if payloadPos < 0 {
		return result, false
	}

	foundWidth := false
	for pos := payloadPos; pos < len(tail); {
		tag := tail[pos]
		switch tag {
		case 0x31, 0x33, 0x34:
			if pos+2 > len(tail) {
				return result, false
			}
			value := tail[pos+1]
			switch tag {
			case 0x31:
				result.SpecialEffect = value
				result.HasSpecialEffect = true
			case 0x33:
				result.BorderStyle = value
				result.HasBorderStyle = true
			case 0x34:
				result.BorderWidth = value
				result.HasBorderWidth = true
			}
			pos += 2
		case 0x60, 0x61, 0x62, 0x63, 0x69:
			if pos+3 > len(tail) {
				return result, false
			}
			value := int(le16(tail[pos+1:]))
			switch tag {
			case 0x60:
				result.Geometry.Left = value
			case 0x61:
				result.Geometry.Top = value
			case 0x62:
				result.Geometry.Width = value
				foundWidth = true
			case 0x63:
				result.Geometry.Height = value
			case 0x69:
				result.TabIndex = value
				result.HasTabIndex = true
			}
			pos += 3
		case 0x9C, 0x9D:
			if pos+5 > len(tail) {
				return result, false
			}
			value := le32(tail[pos+1:])
			if tag == 0x9C {
				result.OptionValue = int(int32(value))
				result.HasOptionValue = true
			} else {
				result.BorderColorValue = value
				result.BorderColor = accessColorHex(value)
				result.HasBorderColor = true
			}
			pos += 5
		default:
			pos++
		}
	}
	if !foundWidth || !result.HasOptionValue ||
		(result.HasSpecialEffect && result.SpecialEffect > 3) ||
		(result.HasBorderStyle && result.BorderStyle > 1) ||
		result.Geometry.Left > 32767 || result.Geometry.Top > 32767 ||
		result.Geometry.Width <= 0 || result.Geometry.Width > 32767 ||
		result.Geometry.Height <= 0 || result.Geometry.Height > 32767 {
		return result, false
	}
	result.HasGeometry = true
	return result, true
}
