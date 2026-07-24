package mdbgo

import (
	"bytes"
	"sort"
	"strconv"
	"strings"
)

func parseJet4ButtonTextProperties(control FormControlInfo, fields []jet4TaggedTextField) []FormProperty {
	var props []FormProperty
	for _, field := range fields {
		switch field.Tag {
		case 0xDD:
			value := field.Value
			trimmed := strings.TrimSpace(value)
			if trimmed != "" && !strings.EqualFold(trimmed, control.Name) && !isKnownFormFont(trimmed) {
				props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0011, value)})
			}
		case 0xDF:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x007E, field.Value)})
		case 0xE4:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0022, field.Value)})
		case 0xEE:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x010A, field.Value)})
		case 0xF1:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x013D, field.Value)})
		}
	}
	return props
}

type jet4ButtonNumericProperties struct {
	TabIndex          int
	HasTabIndex       bool
	BackStyle         byte
	BackColor         string
	BackColorValue    uint32
	Geometry          formControlGeometry
	HasGeometry       bool
	HasExplicitHeight bool
}

func (props jet4ButtonNumericProperties) formProperties() []FormProperty {
	result := []FormProperty{
		{ID: 0x001D, Name: FormPropertyIDToName(0x001D), ValueType: "Byte", Value: strconv.Itoa(int(props.BackStyle))},
		{ID: 0x001C, Name: FormPropertyIDToName(0x001C), ValueType: "Color", Value: props.BackColor},
	}
	if props.HasTabIndex {
		result = append(result, FormProperty{
			ID: 0x0105, Name: FormPropertyIDToName(0x0105), ValueType: "Short", Value: strconv.Itoa(props.TabIndex),
		})
	}
	return result
}

// parseJet4FormButtonProperties 按物理顺序把 Button 专属数值记录配回 Button。
// Button 记录以 FD 68 00 开始，0x60..0x63 保存 twips 几何，0x69 保存 TabIndex。
func parseJet4FormButtonProperties(data []byte, controls []FormControlInfo) map[string]jet4ButtonNumericProperties {
	result := make(map[string]jet4ButtonNumericProperties)
	if len(data) < 8 || len(controls) == 0 || le16(data) > 0x0014 {
		return result
	}

	offsets := orderedFormControlOffsets(data, controls)
	firstControlOffset := len(data)
	for _, offset := range offsets {
		if offset >= 0 && offset < firstControlOffset {
			firstControlOffset = offset
		}
	}
	defaultHeight := parseJet4ButtonDefaultHeight(data[:firstControlOffset])
	type physicalButton struct {
		offset int
		name   string
	}
	buttons := make([]physicalButton, 0)
	for i, offset := range offsets {
		if offset >= 0 && controls[i].Type == "Button" {
			buttons = append(buttons, physicalButton{offset: offset, name: controls[i].Name})
		}
	}
	sort.Slice(buttons, func(i, j int) bool { return buttons[i].offset < buttons[j].offset })

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

	numericByButton := make(map[int]jet4ButtonNumericProperties, len(buttons))
	for _, block := range blocks {
		tail := jet4ControlNumericTailForType(block.block, block.name, block.controlType)
		props, ok := parseJet4ButtonNumericTail(tail)
		if !ok {
			continue
		}
		if !props.HasExplicitHeight {
			props.Geometry.Height = defaultHeight
		}
		target := sort.Search(len(buttons), func(i int) bool {
			return buttons[i].offset > block.offset
		})
		if target < len(buttons) {
			if _, exists := numericByButton[target]; !exists {
				numericByButton[target] = props
			}
		}
	}
	for i, props := range numericByButton {
		result[strings.ToLower(buttons[i].name)] = props
	}
	return result
}

// parseJet4ButtonDefaultHeight 读取命名控件区之前的窗体级 CommandButton 模板。
// 模板同样以 FD 68 00 开始，但不带普通控件的 0x31 布局掩码；若模板省略
// 0x63，则使用 Access 内建的 360-twip 默认高度。
func parseJet4ButtonDefaultHeight(prefix []byte) int {
	const builtInDefaultHeight = 360
	signature := []byte{0xFD, 0x68, 0x00}
	for searchPos := 0; searchPos+len(signature) <= len(prefix); {
		relative := bytes.Index(prefix[searchPos:], signature)
		if relative < 0 {
			break
		}
		recordPos := searchPos + relative
		pos := recordPos + len(signature)
		height := builtInDefaultHeight
		hasHeight := false
		isTemplate := false
		for pos < len(prefix) && pos < recordPos+96 {
			switch prefix[pos] {
			case 0x31:
				pos = len(prefix)
			case 0x62, 0x63, 0x67, 0x68:
				if pos+3 > len(prefix) {
					pos = len(prefix)
					continue
				}
				if prefix[pos] == 0x63 {
					height = int(le16(prefix[pos+1:]))
					hasHeight = height > 0 && height <= 32767
				}
				pos += 3
			case 0x9D:
				if pos+5 > len(prefix) {
					pos = len(prefix)
					continue
				}
				pos += 5
			case 0xE4:
				if pos+2 > len(prefix) {
					pos = len(prefix)
					continue
				}
				byteLen := int(prefix[pos+1])
				if byteLen >= 2 && byteLen%2 == 0 && pos+2+byteLen <= len(prefix) {
					_, isTemplate = decodeJet4UTF16Text(prefix[pos+2 : pos+2+byteLen])
				}
				pos = len(prefix)
			default:
				pos = len(prefix)
			}
		}
		if isTemplate {
			if hasHeight {
				return height
			}
			return builtInDefaultHeight
		}
		searchPos = recordPos + len(signature)
	}
	return builtInDefaultHeight
}

func parseJet4ButtonNumericTail(tail []byte) (jet4ButtonNumericProperties, bool) {
	const defaultButtonBackColor = uint32(0x00FFFFFF)
	result := jet4ButtonNumericProperties{
		BackStyle:      1,
		BackColorValue: defaultButtonBackColor,
		BackColor:      accessColorHex(defaultButtonBackColor),
		Geometry:       formControlGeometry{Width: 1440, Height: 360},
	}
	if len(tail) < 14 {
		return result, false
	}

	payloadPos := -1
	for pos := 0; pos+3 <= len(tail) && pos < 12; pos++ {
		if tail[pos] == 0xFD && tail[pos+1] == 0x68 && tail[pos+2] == 0x00 {
			payloadPos = pos + 3
			break
		}
	}
	// 第一个 Button 可能紧邻 Section 边界；此时记录以 FF 掩码开头，
	// 子类型 68 00 后才是 Button 数值属性。
	if payloadPos < 0 && tail[0] == 0xFF {
		for pos := 1; pos+2 <= len(tail) && pos < 8; pos++ {
			if tail[pos] == 0x68 && tail[pos+1] == 0x00 {
				payloadPos = pos + 2
				break
			}
		}
	}
	if payloadPos < 0 {
		return result, false
	}
	// Height is optional in the compact record. When 0x63 is absent, Access
	// keeps the 360-twip CommandButton default. The 0x31 mask also describes
	// unrelated properties, so values such as F7 must not imply a taller button.
	for pos := payloadPos; pos < len(tail); {
		tag := tail[pos]
		switch tag {
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
			case 0x63:
				result.Geometry.Height = value
				result.HasExplicitHeight = true
			case 0x69:
				result.TabIndex = value
				result.HasTabIndex = true
			}
			pos += 3
		case 0xDC:
			pos = len(tail)
		default:
			pos++
		}
	}
	if result.Geometry.Left > 32767 || result.Geometry.Top > 32767 ||
		result.Geometry.Width <= 0 || result.Geometry.Width > 32767 {
		return result, false
	}
	if result.Geometry.Height <= 0 || result.Geometry.Height > 32767 {
		return result, false
	}
	result.HasGeometry = true
	return result, true
}

func parseJet4ButtonBinaryProperties(control FormControlInfo, block []byte) []FormProperty {
	nameBytes := encodeUTF16LE(control.Name)
	if len(nameBytes) == 0 || !hasUTF16LEPrefixFoldASCII(block, nameBytes) {
		return nil
	}

	var result []FormProperty
	for pos := len(nameBytes); pos+2 <= len(block); {
		tag := block[pos]
		if tag == 0xE3 {
			result = mergeFormProperties(result, []FormProperty{newTextFormProperty(0x0007, "(位图)")})
			break
		}
		byteLen := int(block[pos+1])
		if tag < 0xC0 || byteLen < 2 || byteLen%2 != 0 || pos+2+byteLen > len(block) {
			break
		}
		if _, ok := decodeJet4UTF16Text(block[pos+2 : pos+2+byteLen]); !ok {
			break
		}
		pos += 2 + byteLen
	}

	for _, field := range scanJet4TaggedTextFields(block) {
		if field.Tag == 0xF1 {
			result = mergeFormProperties(result, []FormProperty{newTextFormProperty(0x013D, field.Value)})
		}
	}
	return result
}
