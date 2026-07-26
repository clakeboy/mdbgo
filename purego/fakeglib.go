package purego

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// PtrArray 动态指针数组
type PtrArray struct {
	Data []interface{}
}

// NewPtrArray 创建新的 PtrArray
func NewPtrArray() *PtrArray {
	return &PtrArray{
		Data: make([]interface{}, 0),
	}
}

// Add 添加元素
func (a *PtrArray) Add(item interface{}) {
	a.Data = append(a.Data, item)
}

// Index 获取指定索引的元素
func (a *PtrArray) Index(i int) interface{} {
	if i < 0 || i >= len(a.Data) {
		return nil
	}
	return a.Data[i]
}

// Remove 移除指定元素
func (a *PtrArray) Remove(item interface{}) bool {
	for i, v := range a.Data {
		if v == item {
			a.Data = append(a.Data[:i], a.Data[i+1:]...)
			return true
		}
	}
	return false
}

// Len 返回数组长度
func (a *PtrArray) Len() int {
	return len(a.Data)
}

// Sort 排序数组
func (a *PtrArray) Sort(cmp func(i, j int) bool) {
	// 使用简单的冒泡排序
	n := len(a.Data)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if cmp(j, j+1) {
				a.Data[j], a.Data[j+1] = a.Data[j+1], a.Data[j]
			}
		}
	}
}

// ForEach 遍历数组
func (a *PtrArray) ForEach(fn func(index int, item interface{})) {
	for i, item := range a.Data {
		fn(i, item)
	}
}

// Free 释放数组
func (a *PtrArray) Free() {
	a.Data = nil
}

// HashTable 哈希表
type HashTable struct {
	Items map[string]interface{}
}

// NewHashTable 创建新的 HashTable
func NewHashTable() *HashTable {
	return &HashTable{
		Items: make(map[string]interface{}),
	}
}

// Lookup 查找键值
func (h *HashTable) Lookup(key string) interface{} {
	return h.Items[key]
}

// Insert 插入键值
func (h *HashTable) Insert(key string, value interface{}) {
	h.Items[key] = value
}

// Remove 移除键值
func (h *HashTable) Remove(key string) bool {
	_, exists := h.Items[key]
	if exists {
		delete(h.Items, key)
	}
	return exists
}

// ForEach 遍历哈希表
func (h *HashTable) ForEach(fn func(key, value interface{})) {
	for k, v := range h.Items {
		fn(k, v)
	}
}

// Destroy 销毁哈希表
func (h *HashTable) Destroy() {
	h.Items = nil
}

// String 字符串构建器
type String struct {
	Str          string
	Len          int
	AllocatedLen int
}

// NewString 创建新的 String
func NewString(init string) *String {
	s := &String{
		Str: init,
		Len: len(init),
	}
	s.AllocatedLen = len(init)
	return s
}

// Assign 赋值
func (s *String) Assign(val string) *String {
	s.Str = val
	s.Len = len(val)
	return s
}

// Append 追加字符串
func (s *String) Append(val string) *String {
	s.Str += val
	s.Len = len(s.Str)
	return s
}

// Free 释放字符串
func (s *String) Free() string {
	result := s.Str
	s.Str = ""
	s.Len = 0
	return result
}

// List 链表节点
type List struct {
	Data interface{}
	Next *List
	Prev *List
}

// ListAppend 追加链表节点
func ListAppend(list *List, data interface{}) *List {
	node := &List{Data: data}
	if list == nil {
		return node
	}
	last := list
	for last.Next != nil {
		last = last.Next
	}
	last.Next = node
	node.Prev = last
	return list
}

// ListLast 获取链表最后一个节点
func ListLast(list *List) *List {
	if list == nil {
		return nil
	}
	for list.Next != nil {
		list = list.Next
	}
	return list
}

// ListRemove 移除链表节点
func ListRemove(list *List, data interface{}) *List {
	for node := list; node != nil; node = node.Next {
		if node.Data == data {
			if node.Prev != nil {
				node.Prev.Next = node.Next
			} else {
				list = node.Next
			}
			if node.Next != nil {
				node.Next.Prev = node.Prev
			}
			return list
		}
	}
	return list
}

// ListFree 释放链表
func ListFree(list *List) {
	for list != nil {
		next := list.Next
		list = nil
		list = next
	}
}

// 字符串工具函数

// StrEqual 比较字符串
func StrEqual(str1, str2 string) bool {
	return str1 == str2
}

// StrSplit 分割字符串
func StrSplit(haystack, needle string, maxTokens int) []string {
	if maxTokens <= 0 {
		return strings.Split(haystack, needle)
	}
	return strings.SplitN(haystack, needle, maxTokens)
}

// StrConcat 连接字符串
func StrConcat(parts ...string) string {
	return strings.Join(parts, "")
}

// StrDup 复制字符串
func StrDup(src string) string {
	return src
}

// StrNDup 复制指定长度的字符串
func StrNDup(src string, n int) string {
	if n > len(src) {
		return src
	}
	return src[:n]
}

// StrdupPrintf 格式化字符串
func StrdupPrintf(format string, args ...interface{}) string {
	return strings.TrimSpace(strings.Replace(format, "%", "", -1))
}

// StrDelimit 替换分隔符
func StrDelimit(str, delimiters string, newDelimiter byte) string {
	result := []byte(str)
	for i, c := range result {
		for _, d := range delimiters {
			if byte(d) == c {
				result[i] = newDelimiter
				break
			}
		}
	}
	return string(result)
}

// AsciiStrcasecmp 不区分大小写比较
func AsciiStrcasecmp(s1, s2 string) int {
	s1 = strings.ToLower(s1)
	s2 = strings.ToLower(s2)
	if s1 < s2 {
		return -1
	}
	if s1 > s2 {
		return 1
	}
	return 0
}

// UTF8Casefold UTF-8 大小写折叠
func UTF8Casefold(str string) string {
	return strings.ToLower(str)
}

// UTF8Strdown UTF-8 转小写
func UTF8Strdown(str string) string {
	return strings.ToLower(str)
}

// UnicharToUtf8 Unicode 字符转 UTF-8
func UnicharToUtf8(c rune) string {
	buf := make([]byte, 4)
	n := utf8.EncodeRune(buf, c)
	return string(buf[:n])
}

// LocaleToUtf8 本地编码转 UTF-8
func LocaleToUtf8(str string) (string, error) {
	// 在 Go 中，字符串已经是 UTF-8 编码
	return str, nil
}

// MemDup 内存复制
func MemDup(src interface{}, size int) interface{} {
	return src
}

// GStrHash 字符串哈希
func GStrHash(str string) uint32 {
	var hash uint32 = 5381
	for _, c := range str {
		hash = ((hash << 5) + hash) + uint32(c)
	}
	return hash
}

// 字符串分隔符常量
const StrDelimiters = "_-|> <."

// IsLogicalOp 判断是否为逻辑操作符
func IsLogicalOp(op int) bool {
	return op == MDBOr || op == MDBAnd || op == MDBNot
}

// IsRelationalOp 判断是否为关系操作符
func IsRelationalOp(op int) bool {
	return op == MDBEqual || op == MDBGT || op == MDBLT ||
		op == MDBGTEQ || op == MDBLTEQ || op == MDBNEQ ||
		op == MDBLike || op == MDBILike || op == MDBIsNull || op == MDBNotNull
}

// TrimSpace 去除首尾空白
func TrimSpace(s string) string {
	return strings.TrimSpace(s)
}

// UpperCase 转大写
func UpperCase(s string) string {
	return strings.ToUpper(s)
}

// LowerCase 转小写
func LowerCase(s string) string {
	return strings.ToLower(s)
}

// IsSpace 判断是否为空白字符
func IsSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// IsDigit 判断是否为数字
func IsDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// IsAlpha 判断是否为字母
func IsAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// IsAlnum 判断是否为字母或数字
func IsAlnum(c byte) bool {
	return IsAlpha(c) || IsDigit(c)
}

// ToUpper 转大写
func ToUpper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 32
	}
	return c
}

// ToLower 转小写
func ToLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}

// IsUpper 判断是否为大写
func IsUpper(c byte) bool {
	return c >= 'A' && c <= 'Z'
}

// IsLower 判断是否为小写
func IsLower(c byte) bool {
	return c >= 'a' && c <= 'z'
}

// IsAlphaNum 判断是否为字母或数字
func IsAlphaNum(c byte) bool {
	return IsAlpha(c) || IsDigit(c)
}

// IsPunct 判断是否为标点符号
func IsPunct(c byte) bool {
	return strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", rune(c))
}

// IsControl 判断是否为控制字符
func IsControl(c byte) bool {
	return c < 32 || c == 127
}

// IsPrintable 判断是否为可打印字符
func IsPrintable(c byte) bool {
	return c >= 32 && c < 127
}

// IsGraph 判断是否为图形字符
func IsGraph(c byte) bool {
	return c > 32 && c < 127
}

// IsXDigit 判断是否为十六进制数字
func IsXDigit(c byte) bool {
	return IsDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// ToUpperUnicode Unicode 转大写
func ToUpperUnicode(c rune) rune {
	return unicode.ToUpper(c)
}

// ToLowerUnicode Unicode 转小写
func ToLowerUnicode(c rune) rune {
	return unicode.ToLower(c)
}

// IsSpaceUnicode Unicode 空白判断
func IsSpaceUnicode(c rune) bool {
	return unicode.IsSpace(c)
}

// IsDigitUnicode Unicode 数字判断
func IsDigitUnicode(c rune) bool {
	return unicode.IsDigit(c)
}

// IsLetterUnicode Unicode 字母判断
func IsLetterUnicode(c rune) bool {
	return unicode.IsLetter(c)
}

// IsAlphaUnicode Unicode 字母数字判断
func IsAlphaUnicode(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsDigit(c)
}

// IsUpperUnicode Unicode 大写判断
func IsUpperUnicode(c rune) bool {
	return unicode.IsUpper(c)
}

// IsLowerUnicode Unicode 小写判断
func IsLowerUnicode(c rune) bool {
	return unicode.IsLower(c)
}

// IsTitleUnicode Unicode 标题判断
func IsTitleUnicode(c rune) bool {
	return unicode.IsTitle(c)
}

// IsMarkUnicode Unicode 标记判断
func IsMarkUnicode(c rune) bool {
	return unicode.IsMark(c)
}

// IsSymbolUnicode Unicode 符号判断
func IsSymbolUnicode(c rune) bool {
	return unicode.IsSymbol(c)
}

// IsPunctUnicode Unicode 标点判断
func IsPunctUnicode(c rune) bool {
	return unicode.IsPunct(c)
}

// IsControlUnicode Unicode 控制字符判断
func IsControlUnicode(c rune) bool {
	return unicode.IsControl(c)
}

// IsGraphicUnicode Unicode 图形字符判断
func IsGraphicUnicode(c rune) bool {
	return unicode.IsGraphic(c)
}

// IsPrintUnicode Unicode 可打印字符判断
func IsPrintUnicode(c rune) bool {
	return unicode.IsPrint(rune(c))
}

// IsNumberUnicode Unicode 数字判断
func IsNumberUnicode(c rune) bool {
	return unicode.IsNumber(c)
}

// IsCoderUnicode Unicode 代码判断
func IsCoderUnicode(c rune) bool {
	return unicode.Is(unicode.Cc, c)
}

// IsDashUnicode Unicode 破折号判断
func IsDashUnicode(c rune) bool {
	return unicode.Is(unicode.Pd, c)
}

// IsOtherUnicode Unicode 其他判断
func IsOtherUnicode(c rune) bool {
	return unicode.Is(unicode.C, c)
}

// IsSeparatorUnicode Unicode 分隔符判断
func IsSeparatorUnicode(c rune) bool {
	return unicode.Is(unicode.Z, c)
}

// IsLetterOrNumberUnicode Unicode 字母数字判断
func IsLetterOrNumberUnicode(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsNumber(c)
}

// IsCurrencySymbol Unicode 货币符号判断
func IsCurrencySymbolUnicode(c rune) bool {
	return unicode.Is(unicode.Sc, c)
}

// IsModifierUnicode Unicode 修饰符判断
func IsModifierUnicode(c rune) bool {
	return unicode.Is(unicode.M, c)
}

// IsOtherLetter Unicode 其他字母判断
func IsOtherLetter(c rune) bool {
	return unicode.Is(unicode.Lo, c)
}

// IsNonSpacingMark Unicode 非间距标记判断
func IsNonSpacingMark(c rune) bool {
	return unicode.Is(unicode.Mn, c)
}

// IsEnclosingMark Unicode 封闭标记判断
func IsEnclosingMark(c rune) bool {
	return unicode.Is(unicode.Me, c)
}

// IsCombiningSpacingMark Unicode 组合间距标记判断
func IsCombiningSpacingMark(c rune) bool {
	return unicode.Is(unicode.Mc, c)
}

// IsConnectorPunctuation Unicode 连接标点判断
func IsConnectorPunctuation(c rune) bool {
	return unicode.Is(unicode.Pc, c)
}

// IsOtherPunctuation Unicode 其他标点判断
func IsOtherPunctuation(c rune) bool {
	return unicode.Is(unicode.Po, c)
}

// IsOpenPunctuation Unicode 开放标点判断
func IsOpenPunctuation(c rune) bool {
	return unicode.Is(unicode.Ps, c)
}

// IsClosePunctuation Unicode 关闭标点判断
func IsClosePunctuation(c rune) bool {
	return unicode.Is(unicode.Pe, c)
}

// IsInitialQuote Unicode 初始引号判断
func IsInitialQuote(c rune) bool {
	return unicode.Is(unicode.Pi, c)
}

// IsFinalQuote Unicode 最终引号判断
func IsFinalQuote(c rune) bool {
	return unicode.Is(unicode.Pf, c)
}

// IsMathSymbol Unicode 数学符号判断
func IsMathSymbol(c rune) bool {
	return unicode.Is(unicode.Sm, c)
}

// IsModifierSymbol Unicode 修饰符号判断
func IsModifierSymbol(c rune) bool {
	return unicode.Is(unicode.Sk, c)
}

// IsOtherSymbol Unicode 其他符号判断
func IsOtherSymbol(c rune) bool {
	return unicode.Is(unicode.So, c)
}

// IsLineSeparator Unicode 行分隔符判断
func IsLineSeparator(c rune) bool {
	return unicode.Is(unicode.Zl, c)
}

// IsParagraphSeparator Unicode 段落分隔符判断
func IsParagraphSeparator(c rune) bool {
	return unicode.Is(unicode.Zp, c)
}

// IsSpaceSeparator Unicode 空格分隔符判断
func IsSpaceSeparator(c rune) bool {
	return unicode.Is(unicode.Zs, c)
}

// IsFormat Unicode 格式字符判断
func IsFormat(c rune) bool {
	return unicode.Is(unicode.Cf, c)
}

// IsSurrogate Unicode 代理对判断
func IsSurrogate(c rune) bool {
	return unicode.Is(unicode.Cs, c)
}

// IsPrivateUse Unicode 私用区判断
func IsPrivateUse(c rune) bool {
	return unicode.Is(unicode.Co, c)
}

// IsUnassigned Unicode 未分配判断
func IsUnassigned(c rune) bool {
	return unicode.Is(unicode.Cn, c)
}

// IsAssigned Unicode 已分配判断
func IsAssigned(c rune) bool {
	return !unicode.Is(unicode.Cn, c)
}

// IsLetter Unicode 字母判断
func IsLetter(c rune) bool {
	return unicode.IsLetter(c)
}

// IsNumber Unicode 数字判断
func IsNumber(c rune) bool {
	return unicode.IsNumber(c)
}

// IsMark Unicode 标记判断
func IsMark(c rune) bool {
	return unicode.IsMark(c)
}

// IsUnicodePunct Unicode 标点判断
func IsUnicodePunct(c rune) bool {
	return unicode.IsPunct(c)
}

// IsSymbol Unicode 符号判断
func IsSymbol(c rune) bool {
	return unicode.IsSymbol(c)
}

// IsUnicodeSeparator Unicode 分隔符判断
func IsUnicodeSeparator(c rune) bool {
	return unicode.IsSpace(c)
}

// IsOther Unicode 其他判断
func IsOther(c rune) bool {
	return unicode.Is(unicode.C, c) || unicode.Is(unicode.Z, c) || unicode.Is(unicode.S, c)
}

// IsUnicodeControl Unicode 控制字符判断
func IsUnicodeControl(c rune) bool {
	return unicode.IsControl(c)
}

// IsGraphic Unicode 图形字符判断
func IsGraphic(c rune) bool {
	return unicode.IsGraphic(c)
}

// IsPrint Unicode 可打印字符判断
func IsPrint(c rune) bool {
	return unicode.IsPrint(rune(c))
}

// IsSpaceUnicodeFunc Unicode 空白判断函数
func IsSpaceUnicodeFunc(c rune) bool {
	return unicode.IsSpace(c)
}

// IsDigitFunc 数字判断函数
func IsDigitFunc(c rune) bool {
	return unicode.IsDigit(c)
}

// IsLetterFunc 字母判断函数
func IsLetterFunc(c rune) bool {
	return unicode.IsLetter(c)
}

// IsAlphaFunc 字母数字判断函数
func IsAlphaFunc(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsDigit(c)
}

// IsUpperFunc 大写判断函数
func IsUpperFunc(c rune) bool {
	return unicode.IsUpper(c)
}

// IsLowerFunc 小写判断函数
func IsLowerFunc(c rune) bool {
	return unicode.IsLower(c)
}

// IsTitleFunc 标题判断函数
func IsTitleFunc(c rune) bool {
	return unicode.IsTitle(c)
}

// IsMarkFunc 标记判断函数
func IsMarkFunc(c rune) bool {
	return unicode.IsMark(c)
}

// IsSymbolFunc 符号判断函数
func IsSymbolFunc(c rune) bool {
	return unicode.IsSymbol(c)
}

// IsPunctFunc 标点判断函数
func IsPunctFunc(c rune) bool {
	return unicode.IsPunct(c)
}

// IsControlFunc 控制字符判断函数
func IsControlFunc(c rune) bool {
	return unicode.IsControl(c)
}

// IsGraphicFunc 图形字符判断函数
func IsGraphicFunc(c rune) bool {
	return unicode.IsGraphic(c)
}

// IsPrintFunc 可打印字符判断函数
func IsPrintFunc(c rune) bool {
	return unicode.IsPrint(rune(c))
}

// IsNumberFunc 数字判断函数
func IsNumberFunc(c rune) bool {
	return unicode.IsNumber(c)
}

// IsCoderFunc 代码判断函数
func IsCoderFunc(c rune) bool {
	return unicode.Is(unicode.Cc, c)
}

// IsDashFunc 破折号判断函数
func IsDashFunc(c rune) bool {
	return unicode.Is(unicode.Pd, c)
}

// IsOtherFunc 其他判断函数
func IsOtherFunc(c rune) bool {
	return unicode.Is(unicode.C, c)
}

// IsSeparatorFunc 分隔符判断函数
func IsSeparatorFunc(c rune) bool {
	return unicode.Is(unicode.Z, c)
}

// IsLetterOrNumberFunc 字母数字判断函数
func IsLetterOrNumberFunc(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsNumber(c)
}

// IsCurrencySymbolFunc 货币符号判断函数
func IsCurrencySymbolFunc(c rune) bool {
	return unicode.Is(unicode.Sc, c)
}

// IsModifierFunc 修饰符判断函数
func IsModifierFunc(c rune) bool {
	return unicode.Is(unicode.M, c)
}

// IsOtherLetterFunc 其他字母判断函数
func IsOtherLetterFunc(c rune) bool {
	return unicode.Is(unicode.Lo, c)
}

// IsNonSpacingMarkFunc 非间距标记判断函数
func IsNonSpacingMarkFunc(c rune) bool {
	return unicode.Is(unicode.Mn, c)
}

// IsEnclosingMarkFunc 封闭标记判断函数
func IsEnclosingMarkFunc(c rune) bool {
	return unicode.Is(unicode.Me, c)
}

// IsCombiningSpacingMarkFunc 组合间距标记判断函数
func IsCombiningSpacingMarkFunc(c rune) bool {
	return unicode.Is(unicode.Mc, c)
}

// IsConnectorPunctuationFunc 连接标点判断函数
func IsConnectorPunctuationFunc(c rune) bool {
	return unicode.Is(unicode.Pc, c)
}

// IsOtherPunctuationFunc 其他标点判断函数
func IsOtherPunctuationFunc(c rune) bool {
	return unicode.Is(unicode.Po, c)
}

// IsOpenPunctuationFunc 开放标点判断函数
func IsOpenPunctuationFunc(c rune) bool {
	return unicode.Is(unicode.Ps, c)
}

// IsClosePunctuationFunc 关闭标点判断函数
func IsClosePunctuationFunc(c rune) bool {
	return unicode.Is(unicode.Pe, c)
}

// IsInitialQuoteFunc 初始引号判断函数
func IsInitialQuoteFunc(c rune) bool {
	return unicode.Is(unicode.Pi, c)
}

// IsFinalQuoteFunc 最终引号判断函数
func IsFinalQuoteFunc(c rune) bool {
	return unicode.Is(unicode.Pf, c)
}

// IsMathSymbolFunc 数学符号判断函数
func IsMathSymbolFunc(c rune) bool {
	return unicode.Is(unicode.Sm, c)
}

// IsModifierSymbolFunc 修饰符号判断函数
func IsModifierSymbolFunc(c rune) bool {
	return unicode.Is(unicode.Sk, c)
}

// IsOtherSymbolFunc 其他符号判断函数
func IsOtherSymbolFunc(c rune) bool {
	return unicode.Is(unicode.So, c)
}

// IsLineSeparatorFunc 行分隔符判断函数
func IsLineSeparatorFunc(c rune) bool {
	return unicode.Is(unicode.Zl, c)
}

// IsParagraphSeparatorFunc 段落分隔符判断函数
func IsParagraphSeparatorFunc(c rune) bool {
	return unicode.Is(unicode.Zp, c)
}

// IsSpaceSeparatorFunc 空格分隔符判断函数
func IsSpaceSeparatorFunc(c rune) bool {
	return unicode.Is(unicode.Zs, c)
}

// IsFormatFunc 格式字符判断函数
func IsFormatFunc(c rune) bool {
	return unicode.Is(unicode.Cf, c)
}

// IsSurrogateFunc 代理对判断函数
func IsSurrogateFunc(c rune) bool {
	return unicode.Is(unicode.Cs, c)
}

// IsPrivateUseFunc 私用区判断函数
func IsPrivateUseFunc(c rune) bool {
	return unicode.Is(unicode.Co, c)
}

// IsUnassignedFunc 未分配判断函数
func IsUnassignedFunc(c rune) bool {
	return unicode.Is(unicode.Cn, c)
}

// IsAssignedFunc 已分配判断函数
func IsAssignedFunc(c rune) bool {
	return !unicode.Is(unicode.Cn, c)
}
