package mdbgo

import (
	"sort"
	"strconv"
	"strings"
)

type jet4RectangleNumericProperties struct {
	SpecialEffect    byte
	BackStyle        byte
	BorderStyle      byte
	BorderWidth      byte
	BackColor        string
	BackColorValue   uint32
	BorderColor      string
	BorderColorValue uint32
	Visible          bool
	Geometry         formControlGeometry
	HasGeometry      bool
}

func (props jet4RectangleNumericProperties) formProperties() []FormProperty {
	return []FormProperty{
		{ID: 0x0004, Name: FormPropertyIDToName(0x0004), ValueType: "Byte", Value: strconv.Itoa(int(props.SpecialEffect))},
		{ID: 0x001D, Name: FormPropertyIDToName(0x001D), ValueType: "Byte", Value: strconv.Itoa(int(props.BackStyle))},
		{ID: 0x0009, Name: FormPropertyIDToName(0x0009), ValueType: "Byte", Value: strconv.Itoa(int(props.BorderStyle))},
		{ID: 0x000A, Name: FormPropertyIDToName(0x000A), ValueType: "Byte", Value: strconv.Itoa(int(props.BorderWidth))},
		{ID: 0x001C, Name: FormPropertyIDToName(0x001C), ValueType: "Color", Value: props.BackColor},
		{ID: 0x0008, Name: FormPropertyIDToName(0x0008), ValueType: "Color", Value: props.BorderColor},
		{ID: 0x0094, Name: FormPropertyIDToName(0x0094), ValueType: "Bool", Value: strconv.FormatBool(props.Visible)},
	}
}

// parseJet4FormRectangleProperties 按物理顺序把 FD 65 Rectangle 数值记录配回控件。
func parseJet4FormRectangleProperties(data []byte, controls []FormControlInfo) map[string]jet4RectangleNumericProperties {
	result := make(map[string]jet4RectangleNumericProperties)
	if len(data) < 8 || len(controls) == 0 || le16(data) > 0x0014 {
		return result
	}

	offsets := orderedFormControlOffsets(data, controls)
	type physicalRectangle struct {
		offset int
		name   string
	}
	rectangles := make([]physicalRectangle, 0)
	for i, offset := range offsets {
		if offset >= 0 && controls[i].Type == "Rectangle" {
			rectangles = append(rectangles, physicalRectangle{offset: offset, name: controls[i].Name})
		}
	}
	sort.Slice(rectangles, func(i, j int) bool { return rectangles[i].offset < rectangles[j].offset })

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

	numericRecords := make([]jet4RectangleNumericProperties, 0, len(rectangles))
	for _, block := range blocks {
		tail := jet4ControlNumericTailForType(block.block, block.name, block.controlType)
		props, ok := parseJet4RectangleNumericTail(tail)
		if ok {
			numericRecords = append(numericRecords, props)
		}
	}
	for i := 0; i < len(rectangles) && i < len(numericRecords); i++ {
		result[strings.ToLower(rectangles[i].name)] = numericRecords[i]
	}
	return result
}

func parseJet4RectangleNumericTail(tail []byte) (jet4RectangleNumericProperties, bool) {
	result := jet4RectangleNumericProperties{
		BorderStyle: 1,
		BorderWidth: 1,
		Visible:     true,
		BackColor:   accessColorHex(0),
		BorderColor: accessColorHex(0),
	}
	if len(tail) < 12 {
		return result, false
	}

	payloadPos := -1
	for pos := 0; pos+3 <= len(tail) && pos < 12; pos++ {
		if tail[pos] == 0xFD && tail[pos+1] == 0x65 && tail[pos+2] == 0x00 {
			payloadPos = pos + 3
			break
		}
	}
	// 第一个 Rectangle 若紧邻 TabPage 边界，记录会以 FF 掩码开头，
	// 随后的 65 00 才是 Rectangle 类型标记。
	if payloadPos < 0 && tail[0] == 0xFF {
		for pos := 1; pos+2 <= len(tail) && pos < 8; pos++ {
			if tail[pos] == 0x65 && tail[pos+1] == 0x00 {
				payloadPos = pos + 2
				break
			}
		}
	}
	if payloadPos < 0 {
		return result, false
	}

	foundWidth := false
	foundHeight := false
	for pos := payloadPos; pos < len(tail); {
		tag := tail[pos]
		switch tag {
		case 0x31, 0x32, 0x33, 0x34:
			if pos+2 > len(tail) {
				return result, false
			}
			value := tail[pos+1]
			switch tag {
			case 0x31:
				result.SpecialEffect = value
			case 0x32:
				result.BackStyle = value
			case 0x33:
				result.BorderStyle = value
			case 0x34:
				result.BorderWidth = value
			}
			pos += 2
		case 0x60, 0x61, 0x62, 0x63:
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
				foundHeight = true
			}
			pos += 3
		case 0x9C, 0x9D:
			if pos+5 > len(tail) {
				return result, false
			}
			value := le32(tail[pos+1:])
			if tag == 0x9C {
				result.BackColorValue = value
				result.BackColor = accessColorHex(value)
			} else {
				result.BorderColorValue = value
				result.BorderColor = accessColorHex(value)
			}
			pos += 5
		default:
			pos++
		}
	}
	if !foundWidth || !foundHeight || result.BackStyle > 1 || result.BorderStyle > 1 ||
		result.Geometry.Left > 32767 || result.Geometry.Top > 32767 ||
		result.Geometry.Width <= 0 || result.Geometry.Width > 32767 ||
		result.Geometry.Height <= 0 || result.Geometry.Height > 32767 {
		return result, false
	}
	result.HasGeometry = true
	return result, true
}
