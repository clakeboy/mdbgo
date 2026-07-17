package mdbgo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// FormStreams 是窗体在 MDB 中的原始设计流数据。
type FormStreams struct {
	FormName string
	Lv       []byte
	LvProp   []byte
	LvExtra  []byte
}

// FormLayoutControl 是解析出的控件布局信息。
// HasGeometry 表示四项坐标来自 Jet4 控件块的完整格式标签。
type FormLayoutControl struct {
	Name          string
	Type          string
	Section       string
	IsSection     bool
	Top           int
	Left          int
	Width         int
	Height        int
	HasGeometry   bool
	Caption       string
	ControlSource string
}

// FormLayout 是解析出的窗体布局信息。
type FormLayout struct {
	FormName string
	Width    int
	Controls []FormLayoutControl
}

// ParsedFormPropItem 是从 LvProp 解析出的单个属性项。
type ParsedFormPropItem struct {
	Name      string
	DataType  int
	TextValue string
	RawValue  []byte
}

// ParsedFormPropBlock 是 LvProp 中的一组属性块。
// BlockName 为空时通常表示窗体级属性，非空时可能代表控件或子对象。
type ParsedFormPropBlock struct {
	BlockName string
	Items     []ParsedFormPropItem
}

// ParsedFormProps 是 LvProp 的结构化解析结果。
type ParsedFormProps struct {
	Blocks       []ParsedFormPropBlock
	NameMapNames []string
}

// AccessObjectChunk 表示与指定窗体相关的一个 MSysAccessObjects.Data 分片。
type AccessObjectChunk struct {
	ObjectID int
	Score    int
	Data     []byte
}

// FormDesignChunk 表示筛选后的“窗体设计相关”分片。
type FormDesignChunk struct {
	Chunk      AccessObjectChunk
	StrongHit  bool
	NameHitCnt int
}

// ParseFormLayoutFromObjectStreams 把精确的 TypeInfo 控件目录、常用文本属性和
// Jet4 twips 几何转换成兼容旧 FormLayout API 的结果。
func ParseFormLayoutFromObjectStreams(streams *FormObjectStreams) (*FormLayout, error) {
	content, err := ParseFormContent(streams)
	if err != nil {
		return nil, err
	}
	layout := &FormLayout{
		FormName: content.FormName,
		Width:    content.Width,
		Controls: make([]FormLayoutControl, 0, len(content.Controls)),
	}
	for _, control := range content.Controls {
		layout.Controls = append(layout.Controls, FormLayoutControl{
			Name:          control.Name,
			Type:          control.Type,
			Section:       control.Section,
			IsSection:     control.IsSection,
			Left:          control.Left,
			Top:           control.Top,
			Width:         control.Width,
			Height:        control.Height,
			HasGeometry:   control.HasGeometry,
			Caption:       control.Caption,
			ControlSource: control.ControlSource,
		})
	}
	return layout, nil
}

// ReadFormAccessObjectData 按窗体名读取对应 MSysAccessObjects.Data。
//
// Access 内部会把对象 ID 写在 MSysObjects.Id 中，低 24 位通常对应对象页号。
// 对于本仓库样例库，MSysAccessObjects.ID 与页号存在固定偏移（page-9）。
func (db *DB) ReadFormAccessObjectData(formName string) (*AccessObjectData, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}
	if strings.TrimSpace(formName) == "" {
		return nil, errors.New("form name is empty")
	}

	chunks, err := db.ReadFormAccessObjectChunks(formName)
	if err != nil {
		return nil, err
	}
	if len(chunks) > 0 && chunks[0].Score > 0 {
		return &AccessObjectData{
			ObjectID: chunks[0].ObjectID,
			Data:     chunks[0].Data,
		}, nil
	}

	// 兜底：维持旧的页号映射策略，避免完全失败。
	q, err := db.Query("SELECT Id FROM MSysObjects WHERE Name='" + escapeSQLString(formName) + "' LIMIT 1")
	if err != nil {
		return nil, err
	}
	if len(q.Rows) == 0 || len(q.Rows[0]) == 0 {
		return nil, fmt.Errorf("form not found in MSysObjects: %s", formName)
	}
	msysID, err := strconv.Atoi(strings.TrimSpace(q.Rows[0][0]))
	if err != nil {
		return nil, fmt.Errorf("invalid MSysObjects.Id for form %s: %w", formName, err)
	}
	accessID := accessObjectIDFromMSysID(msysID)
	if accessID < 0 {
		return nil, fmt.Errorf("cannot map MSysObjects.Id to MSysAccessObjects.ID: %d", msysID)
	}
	return db.ReadAccessObjectDataByID(accessID)
}

// ReadFormAccessObjectChunks 返回与指定窗体相关的多个 Data 分片（按相关度降序）。
//
// 这些分片通常共同构成窗体定义的不同部分（对象索引、代码、设计元数据等）。
func (db *DB) ReadFormAccessObjectChunks(formName string) ([]AccessObjectChunk, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}
	if strings.TrimSpace(formName) == "" {
		return nil, errors.New("form name is empty")
	}

	streams, err := db.ReadFormStreams(formName)
	if err != nil {
		return nil, err
	}
	parsed, _ := ParseFormPropsFromLvProp(streams.LvProp)

	ids, err := db.listAccessObjectIDs()
	if err != nil {
		return nil, err
	}

	chunks := make([]AccessObjectChunk, 0, len(ids))
	for _, id := range ids {
		obj, err := db.ReadAccessObjectDataByID(id)
		if err != nil || obj == nil || len(obj.Data) == 0 {
			continue
		}
		score := scoreAccessObjectData(formName, obj.Data, parsed)
		if score <= 0 {
			continue
		}
		chunks = append(chunks, AccessObjectChunk{
			ObjectID: id,
			Score:    score,
			Data:     obj.Data,
		})
	}
	sort.Slice(chunks, func(i, j int) bool {
		if chunks[i].Score == chunks[j].Score {
			return chunks[i].ObjectID < chunks[j].ObjectID
		}
		return chunks[i].Score > chunks[j].Score
	})
	return chunks, nil
}

// ReadFormDesignChunks 返回与窗体设计最相关的分片集合。
//
// 过滤规则：
// 1. 强命中：包含 DocClass=Form_xxx 或 Attribute VB_Name="Form_xxx"
// 2. 弱命中：命中 NameMap 控件名数量 >= 3
func (db *DB) ReadFormDesignChunks(formName string) ([]FormDesignChunk, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}
	if strings.TrimSpace(formName) == "" {
		return nil, errors.New("form name is empty")
	}

	streams, err := db.ReadFormStreams(formName)
	if err != nil {
		return nil, err
	}
	parsed, _ := ParseFormPropsFromLvProp(streams.LvProp)
	chunks, err := db.ReadFormAccessObjectChunks(formName)
	if err != nil {
		return nil, err
	}

	needleDocClass := []byte("docclass=form_" + strings.ToLower(formName))
	needleVBName := []byte("attribute vb_name = \"form_" + strings.ToLower(formName) + "\"")

	out := make([]FormDesignChunk, 0, len(chunks))
	for _, c := range chunks {
		lower := bytes.ToLower(c.Data)
		strong := bytes.Contains(lower, needleDocClass) || bytes.Contains(lower, needleVBName)
		nameHitCnt := countNameMapHitsInBytes(lower, parsed)
		if !strong && nameHitCnt < 3 {
			continue
		}
		out = append(out, FormDesignChunk{
			Chunk:      c,
			StrongHit:  strong,
			NameHitCnt: nameHitCnt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StrongHit != out[j].StrongHit {
			return out[i].StrongHit
		}
		if out[i].NameHitCnt != out[j].NameHitCnt {
			return out[i].NameHitCnt > out[j].NameHitCnt
		}
		if out[i].Chunk.Score != out[j].Chunk.Score {
			return out[i].Chunk.Score > out[j].Chunk.Score
		}
		return out[i].Chunk.ObjectID < out[j].Chunk.ObjectID
	})
	return out, nil
}

// ParseFormLayoutFromStreams 解析窗体二进制流为布局结构（当前为启发式实现）。
//
// 注意：Access 窗体内部格式复杂且版本差异大，该方法优先保证可扩展性，
// 当前仅解析常见字段，可能无法覆盖全部窗体/控件。
func ParseFormLayoutFromStreams(streams *FormStreams) *FormLayout {
	if streams == nil {
		return &FormLayout{}
	}

	layout := &FormLayout{
		FormName: streams.FormName,
	}

	// 优先使用 LvProp 的结构化解析，稳定性优于纯字符串扫描。
	parsed, err := ParseFormPropsFromLvProp(streams.LvProp)
	if err == nil && parsed != nil {
		layout.Width = parsed.findWidth()
		layout.Controls = nameListToControls(parsed.NameMapNames)
		if layout.Width > 0 || len(layout.Controls) > 0 {
			return layout
		}
	}

	// 回退策略：对全部流做 UTF-16 词元扫描，尽可能给出可用结果。
	merged := make([]byte, 0, len(streams.Lv)+len(streams.LvProp)+len(streams.LvExtra))
	merged = append(merged, streams.Lv...)
	merged = append(merged, streams.LvProp...)
	merged = append(merged, streams.LvExtra...)
	words := extractUTF16LEWords(merged, 2)
	layout.Width = guessFormWidth(words)
	layout.Controls = guessControls(words)
	return layout
}

// ParseFormLayoutFromDesignChunks 从设计分片中启发式提取布局信息。
//
// 说明：
// 1. 先用 NameMap 控件名建立控件列表。
// 2. 再在每个分片中定位控件名 UTF-16LE 位置，回看附近字节猜测 Top/Left/Width/Height。
// 3. 该算法不是完整格式解码，结果用于后续精细化解析前的结构重建。
func ParseFormLayoutFromDesignChunks(formName string, chunks []FormDesignChunk, parsed *ParsedFormProps) *FormLayout {
	layout := &FormLayout{FormName: formName}
	if len(chunks) == 0 {
		return layout
	}

	names := []string{}
	if parsed != nil {
		names = append(names, parsed.NameMapNames...)
		layout.Width = parsed.findWidth()
	}
	names = uniqNonEmpty(names)

	ctrlMap := make(map[string]*FormLayoutControl, len(names))
	for _, n := range names {
		if strings.EqualFold(n, "Form") {
			continue
		}
		ctrlMap[n] = &FormLayoutControl{
			Name: n,
			Type: inferControlTypeByName(n),
		}
	}
	if len(ctrlMap) == 0 {
		return layout
	}

	// 每个控件维护多组几何候选，后续统一做全局一致性选优。
	candidateMap := make(map[string][]rectCandidate, len(ctrlMap))

	for _, ch := range chunks {
		data := ch.Chunk.Data
		for name, ctrl := range ctrlMap {
			offsets := findUTF16LETokenOffsets(data, name)
			if len(offsets) == 0 {
				continue
			}

			for _, off := range offsets {
				cands := collectRectCandidatesNearOffset(data, off, layout.Width, ctrl.Type, ch)
				if len(cands) > 0 {
					candidateMap[name] = append(candidateMap[name], cands...)
				}
				if ctrl.Caption == "" {
					if cap := guessCaptionNearOffset(data, off, name); cap != "" {
						ctrl.Caption = cap
					}
				}
			}
		}
	}

	// 用全局 Top/Left 网格分布提升行列对齐稳定性，减少“局部最近值”误判。
	grid := buildRectGridModel(candidateMap)
	for name, ctrl := range ctrlMap {
		best, ok := chooseBestRectCandidate(candidateMap[name], grid)
		if !ok {
			continue
		}
		ctrl.Top = best.Top
		ctrl.Left = best.Left
		ctrl.Width = best.Width
		ctrl.Height = best.Height
	}

	keys := make([]string, 0, len(ctrlMap))
	for k := range ctrlMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	layout.Controls = make([]FormLayoutControl, 0, len(keys))
	for _, k := range keys {
		layout.Controls = append(layout.Controls, *ctrlMap[k])
	}

	// 没有窗体级 Width 时，按控件右边界估算一个可读宽度。
	if layout.Width <= 0 {
		maxRight := 0
		for _, c := range layout.Controls {
			right := c.Left + c.Width
			if right > maxRight {
				maxRight = right
			}
		}
		if maxRight > 0 {
			guess := snapToTwips(maxRight+540, 60)
			if guess > 0 && guess <= 16000 {
				layout.Width = guess
			}
		}
	}
	return layout
}

// ReadAndParseFormLayout 读取窗体设计流并生成兼容 FormLayout 的结果。
// 优先使用内部 OLE Forms/TypeInfo 的精确控件目录；旧分片启发式仅作为兼容兜底。
func (db *DB) ReadAndParseFormLayout(formName string) (*FormLayout, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}
	if strings.TrimSpace(formName) == "" {
		return nil, errors.New("form name is empty")
	}

	objectStreams, objectErr := db.ReadFormObjectStreams(formName)
	if objectErr == nil {
		layout, err := ParseFormLayoutFromObjectStreams(objectStreams)
		if err == nil {
			// 仅当 Jet4 头未提供设计宽度时，才使用旧 LvProp 结果兜底。
			if layout.Width <= 0 {
				streams, streamErr := db.ReadFormStreams(formName)
				if streamErr != nil {
					return layout, nil
				}
				if parsed, parseErr := ParseFormPropsFromLvProp(streams.LvProp); parseErr == nil {
					layout.Width = parsed.findWidth()
				}
			}
			return layout, nil
		}
		objectErr = err
	}

	streams, err := db.ReadFormStreams(formName)
	if err != nil {
		return nil, fmt.Errorf("read OLE form streams: %v; read legacy form streams: %w", objectErr, err)
	}
	parsed, _ := ParseFormPropsFromLvProp(streams.LvProp)
	chunks, err := db.ReadFormDesignChunks(formName)
	if err != nil {
		return nil, err
	}
	return ParseFormLayoutFromDesignChunks(formName, chunks, parsed), nil
}

// ParseFormPropsFromLvProp 解析窗体 LvProp（二进制 KKD/MR2）为结构化属性。
func ParseFormPropsFromLvProp(lvProp []byte) (*ParsedFormProps, error) {
	if len(lvProp) == 0 {
		return &ParsedFormProps{}, nil
	}
	if len(lvProp) < 4 {
		return nil, errors.New("lvProp too short")
	}

	header := string(lvProp[:4])
	if header != "KKD\x00" && header != "MR2\x00" {
		// 某些文件可能没有 NUL 结尾，放宽匹配。
		h := strings.TrimRight(string(lvProp[:4]), "\x00")
		if h != "KKD" && h != "MR2" {
			return nil, fmt.Errorf("unsupported lvProp header: %q", header)
		}
	}

	result := &ParsedFormProps{}
	var nameDefs []string
	pos := 4
	for pos+6 <= len(lvProp) {
		recordLen := int(le32(lvProp[pos:]))
		recordType := int(le16(lvProp[pos+4:]))
		if recordLen <= 0 || pos+recordLen > len(lvProp) {
			break
		}

		payload := lvProp[pos+6 : pos+recordLen]
		switch recordType {
		case 0x80:
			nameDefs = parseKKDNames(payload)
		case 0x00, 0x01, 0x02:
			block := parseKKDBlock(payload, nameDefs)
			result.Blocks = append(result.Blocks, block)
			for _, item := range block.Items {
				if item.Name == "NameMap" && len(item.RawValue) > 0 {
					names := parseNameMapNames(item.RawValue)
					result.NameMapNames = append(result.NameMapNames, names...)
				}
			}
		}
		pos += recordLen
	}

	result.NameMapNames = uniqNonEmpty(result.NameMapNames)
	return result, nil
}

// extractUTF16LEWords 从字节流提取可打印 UTF-16LE 字符串，并按空白/标点拆分词元。
func extractUTF16LEWords(data []byte, minLen int) []string {
	if len(data) < 2 {
		return nil
	}

	var out []string
	var run []uint16
	flush := func() {
		if len(run) < minLen {
			run = run[:0]
			return
		}
		s := string(utf16.Decode(run))
		for _, tok := range splitTokens(s) {
			if tok != "" {
				out = append(out, tok)
			}
		}
		run = run[:0]
	}

	reader := bytes.NewReader(data)
	for {
		var v uint16
		if err := binary.Read(reader, binary.LittleEndian, &v); err != nil {
			break
		}
		if v == 0 {
			flush()
			continue
		}
		// 仅保留常见可见字符，减少误噪声。
		if v >= 32 && v <= 126 {
			run = append(run, v)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// splitTokens 把字符串按空白与常见分隔符拆分。
func splitTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ' ', '\t', '\r', '\n', ',', ';', ':', '{', '}', '[', ']', '(', ')', '"', '\'':
			return true
		default:
			return false
		}
	})
}

// guessFormWidth 从词元中估算窗体宽度。
func guessFormWidth(words []string) int {
	for i := 0; i+1 < len(words); i++ {
		if !strings.EqualFold(words[i], "Width") {
			continue
		}
		if n, err := strconv.Atoi(words[i+1]); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// guessControls 从词元中做启发式控件解析。
func guessControls(words []string) []FormLayoutControl {
	controlTypes := map[string]struct{}{
		"TextBox":   {},
		"CheckBox":  {},
		"Button":    {},
		"Rectangle": {},
		"Line":      {},
		"Label":     {},
		"ComboBox":  {},
	}

	var controls []FormLayoutControl
	cur := FormLayoutControl{}
	hasData := false
	flush := func() {
		if !hasData || cur.Name == "" {
			cur = FormLayoutControl{}
			hasData = false
			return
		}
		controls = append(controls, cur)
		cur = FormLayoutControl{}
		hasData = false
	}

	for i := 0; i < len(words); i++ {
		w := words[i]

		if w == "Name" && i+1 < len(words) {
			flush()
			cur.Name = words[i+1]
			hasData = true
			i++
			continue
		}
		if w == "Type" && i+1 < len(words) {
			// 先尝试读取字符串类型名（如 TextBox）。
			if _, ok := controlTypes[words[i+1]]; ok {
				cur.Type = words[i+1]
				hasData = true
				i++
				continue
			}
			// 再尝试读取数值代码（如 109 -> TextBox）。
			if n, err := strconv.Atoi(words[i+1]); err == nil {
				cur.Type = ControlTypeCodeToString(n)
				hasData = true
			}
			i++
			continue
		}
		// 兼容常见键名变体：ControlType / CType / ctype。
		if strings.EqualFold(w, "ControlType") || strings.EqualFold(w, "CType") {
			if i+1 < len(words) {
				if n, err := strconv.Atoi(words[i+1]); err == nil {
					cur.Type = ControlTypeCodeToString(n)
					hasData = true
					i++
					continue
				}
			}
			i++
			continue
		}
		if w == "Top" && i+1 < len(words) {
			if n, err := strconv.Atoi(words[i+1]); err == nil {
				cur.Top = n
				hasData = true
			}
			i++
			continue
		}
		if w == "Left" && i+1 < len(words) {
			if n, err := strconv.Atoi(words[i+1]); err == nil {
				cur.Left = n
				hasData = true
			}
			i++
			continue
		}
		if w == "Width" && i+1 < len(words) {
			if n, err := strconv.Atoi(words[i+1]); err == nil {
				cur.Width = n
				hasData = true
			}
			i++
			continue
		}
		if w == "Height" && i+1 < len(words) {
			if n, err := strconv.Atoi(words[i+1]); err == nil {
				cur.Height = n
				hasData = true
			}
			i++
			continue
		}
		if w == "Caption" && i+1 < len(words) {
			cur.Caption = words[i+1]
			hasData = true
			i++
			continue
		}
		if w == "ControlSource" && i+1 < len(words) {
			cur.ControlSource = words[i+1]
			hasData = true
			i++
			continue
		}
	}
	flush()
	return controls
}

// findWidth 从窗体级属性里提取 Width。
func (p *ParsedFormProps) findWidth() int {
	if p == nil {
		return 0
	}
	for _, block := range p.Blocks {
		if block.BlockName != "" {
			continue
		}
		for _, item := range block.Items {
			if !strings.EqualFold(item.Name, "Width") {
				continue
			}
			if n, err := strconv.Atoi(strings.TrimSpace(item.TextValue)); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

// nameListToControls 把 NameMap 名称列表转换为控件数组。
func nameListToControls(names []string) []FormLayoutControl {
	names = uniqNonEmpty(names)
	controls := make([]FormLayoutControl, 0, len(names))
	for _, n := range names {
		// NameMap 中会包含 "Form" 这样的保留项，过滤掉。
		if strings.EqualFold(n, "Form") {
			continue
		}
		controls = append(controls, FormLayoutControl{Name: n})
	}
	return controls
}

// parseKKDNames 解析 KKD 名称字典（属性名列表）。
func parseKKDNames(payload []byte) []string {
	var names []string
	pos := 0
	for pos+2 <= len(payload) {
		n := int(le16(payload[pos:]))
		pos += 2
		if n <= 0 || pos+n > len(payload) {
			break
		}
		names = append(names, decodeUTF16LE(payload[pos:pos+n]))
		pos += n
	}
	return names
}

// parseKKDBlock 解析一个属性块记录。
func parseKKDBlock(payload []byte, nameDefs []string) ParsedFormPropBlock {
	block := ParsedFormPropBlock{}
	if len(payload) < 6 {
		return block
	}

	nameLen := int(le16(payload[4:]))
	pos := 6
	if nameLen > 0 && pos+nameLen <= len(payload) {
		block.BlockName = decodeUTF16LE(payload[pos : pos+nameLen])
		pos += nameLen
	}

	for pos+8 <= len(payload) {
		recLen := int(le16(payload[pos:]))
		if recLen <= 0 || pos+recLen > len(payload) {
			break
		}

		dType := int(payload[pos+3])
		elem := int(le16(payload[pos+4:]))
		dSize := int(le16(payload[pos+6:]))
		if dSize < 0 || 8+dSize > recLen || pos+8+dSize > len(payload) {
			break
		}

		itemName := fmt.Sprintf("elem_%d", elem)
		if elem >= 0 && elem < len(nameDefs) && nameDefs[elem] != "" {
			itemName = nameDefs[elem]
		}
		raw := make([]byte, dSize)
		copy(raw, payload[pos+8:pos+8+dSize])
		block.Items = append(block.Items, ParsedFormPropItem{
			Name:      itemName,
			DataType:  dType,
			TextValue: formatKKDValue(dType, raw),
			RawValue:  raw,
		})
		pos += recLen
	}
	return block
}

// formatKKDValue 将 KKD 属性值按类型转成可读字符串。
func formatKKDValue(dtype int, raw []byte) string {
	switch dtype {
	case 0x01: // MDB_BOOL
		if len(raw) > 0 && raw[0] != 0 {
			return "yes"
		}
		return "no"
	case 0x02: // MDB_BYTE
		if len(raw) >= 1 {
			return strconv.Itoa(int(raw[0]))
		}
	case 0x03: // MDB_INT
		if len(raw) >= 2 {
			return strconv.Itoa(int(int16(le16(raw))))
		}
	case 0x04: // MDB_LONGINT
		if len(raw) >= 4 {
			return strconv.Itoa(int(int32(le32(raw))))
		}
	case 0x0a, 0x0c: // MDB_TEXT / MDB_MEMO
		return decodeMaybeUTF16LE(raw)
	case 0x09, 0x0b: // MDB_BINARY / MDB_OLE
		return fmt.Sprintf("(binary data of length %d)", len(raw))
	}
	return decodeMaybeUTF16LE(raw)
}

// parseNameMapNames 从 NameMap 二进制块中提取可能的控件名。
func parseNameMapNames(raw []byte) []string {
	words := extractUTF16LEWords(raw, 3)
	return filterLikelyControlNames(words)
}

// filterLikelyControlNames 过滤并保留像控件名/字段名的 token。
func filterLikelyControlNames(words []string) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		if !isIdentifierLike(w) {
			continue
		}
		out = append(out, w)
	}
	return uniqNonEmpty(out)
}

// isIdentifierLike 判断是否像 Access 对象标识符。
func isIdentifierLike(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r == '_') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// decodeUTF16LE 将字节按 UTF-16LE 解码为字符串。
func decodeUTF16LE(raw []byte) string {
	if len(raw) < 2 {
		return ""
	}
	u16 := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		v := le16(raw[i:])
		if v == 0 {
			break
		}
		u16 = append(u16, v)
	}
	return string(utf16.Decode(u16))
}

// decodeMaybeUTF16LE 优先尝试 UTF-16LE，失败回退 ASCII 可见字符。
func decodeMaybeUTF16LE(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if len(raw)%2 == 0 {
		s := decodeUTF16LE(raw)
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	ascii := make([]byte, 0, len(raw))
	for _, b := range raw {
		if b >= 32 && b <= 126 {
			ascii = append(ascii, b)
		}
	}
	return strings.TrimSpace(string(ascii))
}

// uniqNonEmpty 对字符串去重并保持首次出现顺序。
func uniqNonEmpty(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// accessObjectIDFromMSysID 把 MSysObjects.Id 映射到 MSysAccessObjects.ID。
func accessObjectIDFromMSysID(msysID int) int {
	page := msysID & 0x00FFFFFF
	id := page - 9
	if id < 0 {
		return -1
	}
	return id
}

// scoreAccessObjectData 对候选 Data 行打分，用于选择最可能属于指定窗体的对象。
func scoreAccessObjectData(formName string, data []byte, parsed *ParsedFormProps) int {
	if len(data) == 0 {
		return -1
	}

	score := 0
	words := extractUTF16LEWords(data, 3)
	wordSet := make(map[string]struct{}, len(words))
	for _, w := range words {
		wordSet[strings.ToLower(w)] = struct{}{}
	}

	if _, ok := wordSet[strings.ToLower(formName)]; ok {
		score += 50
	}
	if bytes.Contains(bytes.ToLower(data), []byte(strings.ToLower(formName))) {
		score += 20
	}
	if parsed != nil {
		for _, n := range parsed.NameMapNames {
			if n == "" {
				continue
			}
			if _, ok := wordSet[strings.ToLower(n)]; ok {
				score++
			}
		}
	}
	return score
}

func countNameMapHitsInBytes(lowerData []byte, parsed *ParsedFormProps) int {
	if len(lowerData) == 0 || parsed == nil || len(parsed.NameMapNames) == 0 {
		return 0
	}
	hits := 0
	for _, n := range parsed.NameMapNames {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if bytes.Contains(lowerData, []byte(strings.ToLower(n))) {
			hits++
		}
	}
	return hits
}

// rectCandidate 表示同一控件的一个几何候选。
type rectCandidate struct {
	Top    int
	Left   int
	Width  int
	Height int
	Score  int
}

// rectGridModel 汇总所有候选的全局行列分布，用于二次打分。
type rectGridModel struct {
	TopBuckets  map[int]int
	LeftBuckets map[int]int
}

// collectRectCandidatesNearOffset 在控件名附近采集多个候选矩形，并做初步打分。
func collectRectCandidatesNearOffset(data []byte, tokenOffset int, formWidthHint int, controlType string, chunk FormDesignChunk) []rectCandidate {
	if tokenOffset <= 0 || len(data) < 8 {
		return nil
	}

	from := tokenOffset - 1024
	if from < 0 {
		from = 0
	}
	to := tokenOffset + 256
	if to > len(data)-8 {
		to = len(data) - 8
	}
	if from > to {
		return nil
	}

	out := make([]rectCandidate, 0, 16)
	seen := make(map[[4]int]int, 16)
	for p := from; p+8 <= to; p += 2 {
		a := int(le16(data[p:]))
		b := int(le16(data[p+2:]))
		w := int(le16(data[p+4:]))
		h := int(le16(data[p+6:]))
		dist := absInt(tokenOffset - p)

		// Access 常见既有 Top/Left，也可能出现 Left/Top 排列，二者都尝试。
		tryAddRectCandidate(&out, seen, a, b, w, h, dist, formWidthHint, controlType, chunk.StrongHit)
		tryAddRectCandidate(&out, seen, b, a, w, h, dist, formWidthHint, controlType, chunk.StrongHit)
	}
	// 某些版本/分片会以 32 位整数写入几何值，额外尝试 int32 四元组。
	for p := from; p+16 <= to; p += 2 {
		a := int(int32(le32(data[p:])))
		b := int(int32(le32(data[p+4:])))
		w := int(int32(le32(data[p+8:])))
		h := int(int32(le32(data[p+12:])))
		dist := absInt(tokenOffset - p)
		tryAddRectCandidate(&out, seen, a, b, w, h, dist, formWidthHint, controlType, chunk.StrongHit)
		tryAddRectCandidate(&out, seen, b, a, w, h, dist, formWidthHint, controlType, chunk.StrongHit)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > 24 {
		out = out[:24]
	}
	return out
}

// tryAddRectCandidate 对候选矩形评分，过滤明显噪声后写入列表。
func tryAddRectCandidate(out *[]rectCandidate, seen map[[4]int]int, top, left, width, height, dist, formWidthHint int, controlType string, strongHit bool) {
	score := scoreRectCandidate(top, left, width, height, dist, formWidthHint, controlType, strongHit)
	if score <= 0 {
		return
	}
	key := [4]int{top, left, width, height}
	if old, ok := seen[key]; ok && old >= score {
		return
	}
	seen[key] = score
	*out = append(*out, rectCandidate{
		Top:    top,
		Left:   left,
		Width:  width,
		Height: height,
		Score:  score,
	})
}

// scoreRectCandidate 给单个矩形候选打分，兼顾 twips 网格、控件类型和位置范围。
func scoreRectCandidate(top, left, width, height, dist, formWidthHint int, controlType string, strongHit bool) int {
	if !isLikelyRect(top, left, width, height) {
		return 0
	}
	lowerType := strings.ToLower(controlType)
	if lowerType != "checkbox" && (width < 180 || height < 180) {
		return 0
	}
	if lowerType == "checkbox" && (width < 120 || height < 120) {
		return 0
	}

	score := 120
	score -= dist / 16

	if strongHit {
		score += 20
	}
	if top >= 0 && top <= 12000 {
		score += 10
	}
	if left >= 0 && left <= 12000 {
		score += 10
	}
	if width >= 240 && width <= 12000 {
		score += 12
	}
	if height >= 180 && height <= 3000 {
		score += 12
	}
	if top < 0 || left < 0 {
		score -= 120
	}
	if width > 14000 || height > 8000 {
		score -= 80
	}
	if formWidthHint > 0 && left+width > formWidthHint+540 {
		score -= 70
	}

	// Access 设计器多数几何值落在 30/60 twips 栅格上。
	for _, v := range []int{top, left, width, height} {
		if v%60 == 0 {
			score += 26
			continue
		}
		if v%30 == 0 {
			score += 16
			continue
		}
		if v%15 == 0 {
			score += 6
		}
	}

	switch lowerType {
	case "textbox", "line", "label":
		if height >= 240 && height <= 360 {
			score += 24
		}
		if width >= 240 && width <= 6200 {
			score += 10
		}
	case "button":
		if height >= 240 && height <= 480 {
			score += 22
		}
		if width >= 300 && width <= 2400 {
			score += 10
		}
	case "checkbox":
		if height >= 180 && height <= 420 && width >= 180 && width <= 900 {
			score += 24
		}
	case "rectangle":
		if width >= 600 && height >= 240 {
			score += 14
		}
	default:
		if height >= 180 && height <= 600 {
			score += 8
		}
	}

	// 常见控件更偏好横向布局。
	if lowerType != "checkbox" && height > width {
		score -= 20
	}
	if top > 24000 || left > 24000 {
		score -= 50
	}
	if score < 0 {
		return 0
	}
	return score
}

// buildRectGridModel 汇总候选的 Top/Left 分布，构造全局行列网格。
func buildRectGridModel(candidateMap map[string][]rectCandidate) rectGridModel {
	grid := rectGridModel{
		TopBuckets:  map[int]int{},
		LeftBuckets: map[int]int{},
	}
	for _, cands := range candidateMap {
		if len(cands) == 0 {
			continue
		}
		sort.Slice(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })
		limit := len(cands)
		if limit > 3 {
			limit = 3
		}
		for i := 0; i < limit; i++ {
			c := cands[i]
			topKey := snapToTwips(c.Top, 60)
			leftKey := snapToTwips(c.Left, 60)
			grid.TopBuckets[topKey] += c.Score
			grid.LeftBuckets[leftKey] += c.Score
		}
	}
	return grid
}

// chooseBestRectCandidate 把局部候选与全局网格分数合并，选出最终矩形。
func chooseBestRectCandidate(cands []rectCandidate, grid rectGridModel) (rectCandidate, bool) {
	if len(cands) == 0 {
		return rectCandidate{}, false
	}
	best := cands[0]
	bestScore := -1 << 30
	for _, c := range cands {
		topKey := snapToTwips(c.Top, 60)
		leftKey := snapToTwips(c.Left, 60)
		total := c.Score
		total += grid.TopBuckets[topKey] / 24
		total += grid.LeftBuckets[leftKey] / 28
		if total > bestScore {
			bestScore = total
			best = c
		}
	}
	return best, true
}

// snapToTwips 按给定步长四舍五入到 twips 栅格。
func snapToTwips(v, step int) int {
	if step <= 0 {
		return v
	}
	half := step / 2
	if v >= 0 {
		return ((v + half) / step) * step
	}
	return ((v - half) / step) * step
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func guessRectNearOffset(data []byte, tokenOffset int) (top, left, width, height int, ok bool) {
	if tokenOffset <= 0 || len(data) < 8 {
		return 0, 0, 0, 0, false
	}
	from := tokenOffset - 128
	if from < 0 {
		from = 0
	}
	to := tokenOffset
	bestDist := 1 << 30

	for p := from; p+8 <= to; p += 2 {
		t := int(le16(data[p:]))
		l := int(le16(data[p+2:]))
		w := int(le16(data[p+4:]))
		h := int(le16(data[p+6:]))
		if !isLikelyRect(t, l, w, h) {
			continue
		}
		dist := tokenOffset - p
		if dist < bestDist {
			bestDist = dist
			top, left, width, height = t, l, w, h
			ok = true
		}
	}
	return top, left, width, height, ok
}

func isLikelyRect(top, left, width, height int) bool {
	if top < 0 || left < 0 || width <= 0 || height <= 0 {
		return false
	}
	if top > 30000 || left > 30000 || width > 30000 || height > 12000 {
		return false
	}
	return true
}

func guessCaptionNearOffset(data []byte, tokenOffset int, name string) string {
	if len(data) < 2 {
		return ""
	}
	from := tokenOffset
	to := tokenOffset + 220
	if to > len(data) {
		to = len(data)
	}
	words := extractUTF16LEWords(data[from:to], 2)
	nameLower := strings.ToLower(name)
	for _, w := range words {
		lw := strings.ToLower(strings.TrimSpace(w))
		if lw == "" || lw == nameLower {
			continue
		}
		if isIdentifierLike(w) {
			continue
		}
		if len(w) > 64 {
			continue
		}
		return w
	}
	return ""
}
