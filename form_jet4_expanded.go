package mdbgo

import (
	"sort"
	"strings"
)

type jet4ExpandedEvent struct {
	offset int
	data   []byte
}

type jet4ExpandedRange struct {
	start int
	end   int
}

const jet4ExpandedControlAnchor = "__mdbgo_anchor__"

type jet4ExpandedField struct {
	tag        byte
	propertyID uint16
	valueType  uint32
	value      []byte
	start      int
	valueStart int
	end        int
}

type jet4ExpandedNamedRecord struct {
	offset     int
	recordType uint16
	name       string
	compact    []byte
}

type jet4ExpandedNumericSet struct {
	textBoxes     map[string]jet4FormNumericProperties
	labels        map[string]jet4LabelNumericProperties
	comboBoxes    map[string]jet4ComboBoxNumericProperties
	buttons       map[string]jet4ButtonNumericProperties
	checkBoxes    map[string]jet4CheckBoxNumericProperties
	rectangles    map[string]jet4RectangleNumericProperties
	optionGroups  map[string]jet4OptionGroupNumericProperties
	optionButtons map[string]jet4OptionButtonNumericProperties
	subForms      map[string]jet4SubFormNumericProperties
	tabControls   map[string]jet4TabControlNumericProperties
	tabPages      map[string]jet4TabPageNumericProperties
	sections      map[string]jet4SectionProperties
	geometries    map[string]formControlGeometry
}

// normalizeJet4ExpandedFormBlob 把 Access 2003 使用的 0x0014 展开记录
// 还原为 Access 2000 0x0013 Blob 中的紧凑标签流。两种格式保存的是同一组
// Form/Section/Control 属性；展开格式为每个标签额外保存属性 ID、类型和长度。
//
// 返回的数据只供现有 Jet4 属性解释器使用，FormControlContent.BlobOffset
// 仍保留原始 Blob 中的位置。
func normalizeJet4ExpandedFormBlob(data []byte, controls []FormControlInfo) []byte {
	if len(data) < 10 || le16(data) != 0x0014 {
		return data
	}

	// 紧凑格式头部同样为 8 字节，随后是 Form 类型和标签字段。
	prefix := []byte{0x13, 0x00, 0x13, 0x00, 0x00, 0x00, 0x00, 0x00}
	covered := make([]jet4ExpandedRange, 0, len(controls)+1)
	formType := le16(data[8:])
	if fields, end, ok := compactJet4ExpandedFields(data, 10); ok {
		prefix = append(prefix, byte(formType), byte(formType>>8))
		prefix = append(prefix, fields...)
		covered = append(covered, jet4ExpandedCoveredRanges(data, 10, end)...)
	}

	events := make([]jet4ExpandedEvent, 0, len(controls)*2)
	// FD/FE/FF 开头的是 Form/控件模板或数值记录。展开格式把 marker
	// 保存为 DWORD，后跟原紧凑格式中的 uint16 记录类型。
	for pos := 10; pos+6 <= len(data); pos++ {
		marker := data[pos]
		if (marker != 0xFD && marker != 0xFE && marker != 0xFF) ||
			data[pos+1] != 0 || data[pos+2] != 0 || data[pos+3] != 0 {
			continue
		}
		fieldsStart := pos + 6
		fields, end, ok := compactJet4ExpandedFields(data, fieldsStart)
		var nestedType []byte
		if !ok && pos+8 <= len(data) {
			// FF Section 边界记录会在 marker 类型后再保存一个 uint16
			// 子类型，例如 FF 09 00 70 00 ... 表示其后为 SubForm 记录。
			fieldsStart = pos + 8
			fields, end, ok = compactJet4ExpandedFields(data, fieldsStart)
			if ok {
				nestedType = append(nestedType, data[pos+6:pos+8]...)
			}
		}
		if !ok {
			continue
		}
		recordType := le16(data[pos+4:])
		eventData := []byte{marker, byte(recordType), byte(recordType >> 8)}
		eventData = append(eventData, nestedType...)
		eventData = append(eventData, fields...)
		events = append(events, jet4ExpandedEvent{offset: pos, data: eventData})
		covered = append(covered, jet4ExpandedCoveredRanges(data, fieldsStart, end)...)
	}

	// 不在已规范化字段值中的物理名称块需要单独保留。
	controlOffsets := orderedFormControlOffsets(data, controls)
	seenControlOffsets := make(map[int]bool, len(controlOffsets))
	for i, offset := range controlOffsets {
		if offset < 0 || seenControlOffsets[offset] || jet4ExpandedOffsetCovered(offset, covered) {
			continue
		}
		seenControlOffsets[offset] = true
		nameBytes := encodeUTF16LE(controls[i].Name)
		if offset+len(nameBytes) > len(data) {
			continue
		}
		fields, _, _ := compactJet4ExpandedFields(data, offset+len(nameBytes))
		eventData := append([]byte(nil), nameBytes...)
		eventData = append(eventData, fields...)
		events = append(events, jet4ExpandedEvent{offset: offset, data: eventData})
	}

	result := buildJet4ExpandedNormalizedBlob(prefix, events)
	normalizedOffsets := orderedFormControlOffsets(result, controls)

	// 大型图片或页面设置可能使真实控件名位于一个被压缩跳过的 Binary 值中。
	// 仅为第一轮未定位到的控件补高分锚点，避免改变已正确匹配控件的物理顺序。
	anchorBytes := encodeUTF16LE(jet4ExpandedControlAnchor)
	anchoredOffsets := make(map[int]bool)
	for i, normalizedOffset := range normalizedOffsets {
		offset := controlOffsets[i]
		if normalizedOffset >= 0 || offset < 0 || anchoredOffsets[offset] {
			continue
		}
		anchoredOffsets[offset] = true
		nameBytes := encodeUTF16LE(controls[i].Name)
		if offset+len(nameBytes) > len(data) {
			continue
		}
		fields, _, _ := compactJet4ExpandedFields(data, offset+len(nameBytes))
		eventData := append([]byte(nil), nameBytes...)
		eventData = append(eventData, 0xC1, byte(len(anchorBytes)))
		eventData = append(eventData, anchorBytes...)
		eventData = append(eventData, fields...)
		events = append(events, jet4ExpandedEvent{offset: offset, data: eventData})
	}
	if len(anchoredOffsets) > 0 {
		result = buildJet4ExpandedNormalizedBlob(prefix, events)
	}
	return result
}

func buildJet4ExpandedNormalizedBlob(prefix []byte, events []jet4ExpandedEvent) []byte {
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].offset < events[j].offset
	})
	result := append([]byte(nil), prefix...)
	for _, event := range events {
		result = append(result, event.data...)
	}
	return result
}

func jet4ExpandedOffsetCovered(offset int, ranges []jet4ExpandedRange) bool {
	for _, item := range ranges {
		if offset >= item.start && offset < item.end {
			return true
		}
	}
	return false
}

// compactJet4ExpandedFields 读取一串连续的 Access 2003 展开标签。
//
// 每项布局为：
//
//	uint32 tag
//	uint16 property_id
//	uint32 value_type
//	uint32 element_size
//	uint32 byte_length
//	byte[byte_length] value
func compactJet4ExpandedFields(data []byte, pos int) ([]byte, int, bool) {
	fields, end, ok := readJet4ExpandedFields(data, pos)
	if !ok {
		return nil, pos, false
	}
	result := make([]byte, 0, 64)
	for _, field := range fields {
		value := field.value
		switch field.valueType {
		case 0x09, 0x0A, 0x0B, 0x0C:
			if len(value) > 0xFF {
				// 紧凑格式只有 1 字节长度。大图片等字段与布局解析无关，
				// 跳过其内容后继续保留同一控件后面的数值记录。
				continue
			}
			result = append(result, field.tag, byte(len(value)))
		default:
			result = append(result, field.tag)
		}
		result = append(result, value...)
	}
	return result, end, true
}

func readJet4ExpandedFields(data []byte, pos int) ([]jet4ExpandedField, int, bool) {
	result := make([]jet4ExpandedField, 0, 8)
	for pos+18 <= len(data) {
		tagValue := le32(data[pos:])
		if tagValue > 0xFF {
			break
		}
		tag := byte(tagValue)
		propertyID := le16(data[pos+4:])
		valueType := le32(data[pos+6:])
		byteLength := uint64(le32(data[pos+14:]))
		if propertyID == 0 || !isJet4ExpandedValueType(valueType) ||
			byteLength > uint64(len(data)-(pos+18)) {
			break
		}
		valueEnd := pos + 18 + int(byteLength)
		result = append(result, jet4ExpandedField{
			tag:        tag,
			propertyID: propertyID,
			valueType:  valueType,
			value:      data[pos+18 : valueEnd],
			start:      pos,
			valueStart: pos + 18,
			end:        valueEnd,
		})
		pos = valueEnd
	}
	return result, pos, len(result) > 0
}

func jet4ExpandedCoveredRanges(data []byte, pos, expectedEnd int) []jet4ExpandedRange {
	fields, end, ok := readJet4ExpandedFields(data, pos)
	if !ok || end != expectedEnd {
		return nil
	}
	result := make([]jet4ExpandedRange, 0, len(fields))
	for _, field := range fields {
		if field.valueType == 0x0B && len(field.value) > 0xFF {
			// 大 Binary 值可能是 TabControl 等容器的嵌套布局流。只屏蔽
			// 展开字段头，值内的控件名和子记录必须继续参与物理顺序还原。
			result = append(result, jet4ExpandedRange{start: field.start, end: field.valueStart})
			continue
		}
		result = append(result, jet4ExpandedRange{start: field.start, end: field.end})
	}
	return result
}

func parseJet4ExpandedFormTextProperties(data []byte) []FormProperty {
	if len(data) < 10 || le16(data) != 0x0014 {
		return nil
	}
	fields, _, ok := readJet4ExpandedFields(data, 10)
	if !ok {
		return nil
	}
	var result []FormProperty
	for _, field := range fields {
		if field.valueType != 0x0A && field.valueType != 0x0C {
			continue
		}
		value := decodeUTF16LE(field.value)
		switch field.propertyID {
		case 0x0011:
			if strings.TrimSpace(value) != "" {
				result = mergeFormProperties(result, []FormProperty{
					newTextFormProperty(field.propertyID, value),
				})
			}
		case 0x009C:
			if source, ok := normalizeTaggedRecordSource(value); ok {
				result = mergeFormProperties(result, []FormProperty{
					newTextFormProperty(field.propertyID, source),
				})
			}
		}
	}
	return result
}

func isJet4ExpandedValueType(valueType uint32) bool {
	switch valueType {
	case 0x01, 0x02, 0x03, 0x04, 0x06, 0x08, 0x09, 0x0A, 0x0B, 0x0C:
		return true
	default:
		return false
	}
}

func parseJet4ExpandedNamedRecords(data []byte) []jet4ExpandedNamedRecord {
	result := make([]jet4ExpandedNamedRecord, 0)
	if len(data) < 10 || le16(data) != 0x0014 {
		return result
	}
	for pos := 10; pos+6 <= len(data); pos++ {
		marker := data[pos]
		if (marker != 0xFD && marker != 0xFE && marker != 0xFF) ||
			data[pos+1] != 0 || data[pos+2] != 0 || data[pos+3] != 0 {
			continue
		}
		fieldsStart := pos + 6
		fields, _, ok := readJet4ExpandedFields(data, fieldsStart)
		var nestedType []byte
		if !ok && pos+8 <= len(data) {
			fieldsStart = pos + 8
			fields, _, ok = readJet4ExpandedFields(data, fieldsStart)
			if ok {
				nestedType = append(nestedType, data[pos+6:pos+8]...)
			}
		}
		if !ok {
			continue
		}

		recordType := le16(data[pos+4:])
		effectiveType := recordType
		if len(nestedType) == 2 {
			effectiveType = le16(nestedType)
		}
		compact := []byte{marker, byte(recordType), byte(recordType >> 8)}
		compact = append(compact, nestedType...)
		name := ""
		for _, field := range fields {
			if field.propertyID == 0x0014 &&
				(field.valueType == 0x0A || field.valueType == 0x0C) {
				name = strings.TrimSpace(decodeUTF16LE(field.value))
				break
			}
			switch field.valueType {
			case 0x09, 0x0A, 0x0B, 0x0C:
				if len(field.value) > 0xFF {
					continue
				}
				compact = append(compact, field.tag, byte(len(field.value)))
			default:
				compact = append(compact, field.tag)
			}
			compact = append(compact, field.value...)
		}
		result = append(result, jet4ExpandedNamedRecord{
			offset:     pos,
			recordType: effectiveType,
			name:       name,
			compact:    compact,
		})
	}
	return result
}

func jet4ExpandedFormControlOffsets(
	data []byte,
	controls []FormControlInfo,
	fallback []int,
) []int {
	result := append([]int(nil), fallback...)
	offsetsByControl := make(map[string][]int)
	for _, record := range parseJet4ExpandedNamedRecords(data) {
		if record.name == "" || record.recordType == 0 {
			continue
		}
		key := strings.ToLower(record.name) + "\x00" + string(rune(record.recordType))
		offsetsByControl[key] = append(offsetsByControl[key], record.offset)
	}
	used := make(map[string]int, len(offsetsByControl))
	for index, control := range controls {
		key := strings.ToLower(control.Name) + "\x00" + string(rune(control.TypeCode&0xFF))
		position := used[key]
		offsets := offsetsByControl[key]
		if position >= len(offsets) {
			continue
		}
		result[index] = offsets[position]
		used[key] = position + 1
	}
	return result
}

func parseJet4ExpandedNumericSet(
	data, normalized []byte,
	controls []FormControlInfo,
) jet4ExpandedNumericSet {
	result := jet4ExpandedNumericSet{
		textBoxes:     make(map[string]jet4FormNumericProperties),
		labels:        make(map[string]jet4LabelNumericProperties),
		comboBoxes:    make(map[string]jet4ComboBoxNumericProperties),
		buttons:       make(map[string]jet4ButtonNumericProperties),
		checkBoxes:    make(map[string]jet4CheckBoxNumericProperties),
		rectangles:    make(map[string]jet4RectangleNumericProperties),
		optionGroups:  make(map[string]jet4OptionGroupNumericProperties),
		optionButtons: make(map[string]jet4OptionButtonNumericProperties),
		subForms:      make(map[string]jet4SubFormNumericProperties),
		tabControls:   make(map[string]jet4TabControlNumericProperties),
		tabPages:      make(map[string]jet4TabPageNumericProperties),
		sections:      make(map[string]jet4SectionProperties),
		geometries:    make(map[string]formControlGeometry),
	}

	buttonDefaultHeight := parseJet4ButtonDefaultHeight(normalized)
	optionButtonDefaultHeight := parseJet4OptionButtonDefaultHeight(normalized)
	comboBoxDefaultWidth := parseJet4ComboBoxDefaultWidth(normalized)
	rectangleDefaultHeight := parseJet4RectangleDefaultHeight(normalized)
	labelNames := make([]string, 0)
	labelValues := make(map[int]jet4LabelNumericProperties)
	textBoxNames := make([]string, 0)
	textBoxValues := make(map[int]jet4FormNumericProperties)

	sectionNames := make(map[uint16]string, 3)
	for _, control := range controls {
		switch control.TypeCode {
		case 0x1898, 0x1899, 0x189A:
			sectionNames[control.TypeCode&0xFF] = strings.ToLower(control.Name)
		}
	}

	for _, record := range parseJet4ExpandedNamedRecords(data) {
		name := strings.ToLower(record.name)
		if name == "" {
			continue
		}
		if geometry, ok := parseJet4ControlGeometry(record.compact); ok {
			result.geometries[name] = geometry
		}
		switch record.recordType {
		case 0x64:
			if props, ok := parseJet4LabelNumericTail(record.compact); ok {
				labelValues[len(labelNames)] = props
				labelNames = append(labelNames, name)
			}
		case 0x65:
			if props, ok := parseJet4RectangleNumericTailWithDefaultHeight(
				record.compact, rectangleDefaultHeight); ok {
				result.rectangles[name] = props
			}
		case 0x67:
			if props, ok := parseJet4CheckBoxNumericTail(record.compact); ok {
				result.checkBoxes[name] = props
			}
		case 0x68:
			if props, ok := parseJet4ButtonNumericTail(record.compact); ok {
				if !props.HasExplicitHeight {
					props.Geometry.Height = buttonDefaultHeight
				}
				result.buttons[name] = props
			}
		case 0x69:
			if props, ok := parseJet4OptionButtonNumericTailWithDefaultHeight(
				record.compact, optionButtonDefaultHeight); ok {
				result.optionButtons[name] = props
			}
		case 0x6B:
			if props, ok := parseJet4OptionGroupNumericTail(record.compact); ok {
				result.optionGroups[name] = props
			}
		case 0x6D:
			if props, ok := parseJet4TextBoxNumericTail(record.compact); ok {
				textBoxValues[len(textBoxNames)] = props
				textBoxNames = append(textBoxNames, name)
			}
		case 0x6F:
			if props, ok := parseJet4ComboBoxNumericTailWithDefaultWidth(
				record.compact, comboBoxDefaultWidth); ok {
				result.comboBoxes[name] = props
			}
		case 0x70:
			if props, ok := parseJet4SubFormNumericTail(record.compact); ok {
				result.subForms[name] = props
			}
		case 0x7B:
			if props, ok := parseJet4TabControlNumericTail(record.compact); ok {
				result.tabControls[name] = props
			}
		case 0x7C:
			if props, ok := parseJet4TabPageNumericTail(record.compact); ok {
				result.tabPages[name] = props
			}
		case 0x98, 0x99, 0x9A:
			if props, ok := parseJet4SectionRecord(record.compact); ok {
				sectionName := sectionNames[record.recordType]
				if sectionName == "" {
					sectionName = name
				}
				result.sections[sectionName] = props
			}
		}
	}

	applyJet4LabelColorDefaults(labelValues)
	for index, name := range labelNames {
		result.labels[name] = labelValues[index]
	}
	applyJet4TextBoxColorDefaults(textBoxValues)
	for index, name := range textBoxNames {
		result.textBoxes[name] = textBoxValues[index]
	}
	return result
}
