package mdbgo

import (
	"strconv"
	"strings"
)

// jet4SectionProperties 保存 Access Form Section 记录中已经验证的属性。
// Section 只有高度，没有普通控件的 Left/Top/Width 几何。
type jet4SectionProperties struct {
	Height          int
	BackColor       string
	BackColorValue  uint32
	SpecialEffect   byte
	Visible         bool
	EventProcPrefix string
}

func (props jet4SectionProperties) formProperties() []FormProperty {
	result := []FormProperty{
		{ID: 0x0004, Name: FormPropertyIDToName(0x0004), ValueType: "Byte", Value: strconv.Itoa(int(props.SpecialEffect))},
		{ID: 0x002C, Name: FormPropertyIDToName(0x002C), ValueType: "Short", Value: strconv.Itoa(props.Height)},
		{ID: 0x001C, Name: FormPropertyIDToName(0x001C), ValueType: "Color", Value: props.BackColor},
		{ID: 0x0094, Name: FormPropertyIDToName(0x0094), ValueType: "Bool", Value: strconv.FormatBool(props.Visible)},
	}
	if props.EventProcPrefix != "" {
		result = append(result, newTextFormProperty(0x0016, props.EventProcPrefix))
	}
	return result
}

// parseJet4FormSectionProperties 解析 FormHeader、Detail、FormFooter 的 FD 99/98/9A 记录。
// Section 的物理记录可能位于所属控件之后，因此按记录类型匹配 TypeInfo，而不依赖名称偏移。
func parseJet4FormSectionProperties(data []byte, controls []FormControlInfo) map[string]jet4SectionProperties {
	result := make(map[string]jet4SectionProperties)
	if len(data) < 12 || len(controls) == 0 || le16(data) > 0x0014 {
		return result
	}

	sectionNames := make(map[byte]string, 3)
	for _, control := range controls {
		switch control.TypeCode {
		case 0x1898, 0x1899, 0x189A:
			sectionNames[byte(control.TypeCode)] = strings.ToLower(control.Name)
		}
	}
	if len(sectionNames) == 0 {
		return result
	}

	for pos := 0; pos+3 <= len(data); pos++ {
		if data[pos] != 0xFD && data[pos] != 0xFE {
			continue
		}
		sectionName, ok := sectionNames[data[pos+1]]
		if !ok || data[pos+2] != 0 {
			continue
		}
		props, ok := parseJet4SectionRecord(data[pos:])
		if !ok {
			continue
		}
		result[sectionName] = props
	}
	return result
}

func parseJet4SectionRecord(record []byte) (jet4SectionProperties, bool) {
	result := jet4SectionProperties{Visible: true}
	if len(record) < 12 || (record[0] != 0xFD && record[0] != 0xFE) ||
		record[2] != 0 || (record[1] != 0x98 && record[1] != 0x99 && record[1] != 0x9A) {
		return result, false
	}

	limit := len(record)
	if limit > 256 {
		limit = 256
	}
	foundHeight := false
	foundBackColor := false
	for pos := 3; pos < limit; {
		tag := record[pos]
		switch tag {
		case 0x33:
			// Section 的紧凑属性表中 0x33 对应 SpecialEffect。
			if pos+2 > limit || record[pos+1] > 3 {
				return result, false
			}
			result.SpecialEffect = record[pos+1]
			pos += 2
		case 0x60:
			if pos+3 > limit {
				return result, false
			}
			result.Height = int(le16(record[pos+1:]))
			foundHeight = true
			pos += 3
		case 0x9C:
			if pos+5 > limit {
				return result, false
			}
			result.BackColorValue = le32(record[pos+1:])
			result.BackColor = accessColorHex(result.BackColorValue)
			foundBackColor = true
			pos += 5
		case 0xDF:
			if !foundBackColor || pos+2 > limit {
				pos++
				continue
			}
			byteLen := int(record[pos+1])
			if byteLen < 2 || byteLen%2 != 0 || pos+2+byteLen > limit {
				return result, false
			}
			if value, ok := decodeJet4UTF16Text(record[pos+2 : pos+2+byteLen]); ok {
				result.EventProcPrefix = value
			}
			pos += 2 + byteLen
		case 0xE7:
			// Section EventProcPrefix 后固定跟随 16 字节对象 GUID；到此记录已完整。
			if pos+18 <= limit && record[pos+1] == 16 {
				return result, foundHeight && foundBackColor && result.Height <= 32767
			}
			pos++
		case 0xFD, 0xFE, 0xFF:
			return result, foundHeight && foundBackColor && result.Height <= 32767
		default:
			pos++
		}
	}
	return result, foundHeight && foundBackColor && result.Height <= 32767
}
