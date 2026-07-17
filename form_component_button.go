package mdbgo

import (
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
	TabIndex       int
	HasTabIndex    bool
	BackStyle      byte
	BackColor      string
	BackColorValue uint32
	Geometry       formControlGeometry
	HasGeometry    bool
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

	numericRecords := make([]jet4ButtonNumericProperties, 0, len(buttons))
	for _, block := range blocks {
		tail := jet4ControlNumericTailForType(block.block, block.name, block.controlType)
		props, ok := parseJet4ButtonNumericTail(tail)
		if ok {
			numericRecords = append(numericRecords, props)
		}
	}
	for i := 0; i < len(buttons) && i < len(numericRecords); i++ {
		result[strings.ToLower(buttons[i].name)] = numericRecords[i]
	}
	return result
}

func parseJet4ButtonNumericTail(tail []byte) (jet4ButtonNumericProperties, bool) {
	const defaultButtonBackColor = uint32(0x00FFFFFF)
	result := jet4ButtonNumericProperties{
		BackStyle:      1,
		BackColorValue: defaultButtonBackColor,
		BackColor:      accessColorHex(defaultButtonBackColor),
		Geometry:       formControlGeometry{Height: 360},
	}
	if len(tail) < 14 {
		return result, false
	}

	recordPos := -1
	for pos := 0; pos+3 <= len(tail) && pos < 12; pos++ {
		if tail[pos] == 0xFD && tail[pos+1] == 0x68 && tail[pos+2] == 0x00 {
			recordPos = pos
			break
		}
	}
	if recordPos < 0 {
		return result, false
	}

	layoutPos := -1
	for pos := recordPos + 3; pos+9 <= len(tail); pos++ {
		if tail[pos] == 0x60 && tail[pos+3] == 0x61 && tail[pos+6] == 0x62 {
			layoutPos = pos
			break
		}
	}
	if layoutPos < 0 {
		return result, false
	}

	result.Geometry.Left = int(le16(tail[layoutPos+1:]))
	result.Geometry.Top = int(le16(tail[layoutPos+4:]))
	result.Geometry.Width = int(le16(tail[layoutPos+7:]))
	if result.Geometry.Left > 32767 || result.Geometry.Top > 32767 ||
		result.Geometry.Width <= 0 || result.Geometry.Width > 32767 {
		return result, false
	}
	pos := layoutPos + 9
	if pos+3 <= len(tail) && tail[pos] == 0x63 {
		result.Geometry.Height = int(le16(tail[pos+1:]))
		pos += 3
	}
	if result.Geometry.Height <= 0 || result.Geometry.Height > 32767 {
		return result, false
	}
	result.HasGeometry = true

	for pos < len(tail) {
		if tail[pos] == 0x69 {
			if pos+3 > len(tail) {
				return result, false
			}
			result.TabIndex = int(le16(tail[pos+1:]))
			result.HasTabIndex = true
			pos += 3
			continue
		}
		pos++
	}
	return result, true
}
