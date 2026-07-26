package purego

import "fmt"

// MoneyToString 将货币值转换为字符串
func (mdb *MdbHandle) MoneyToString(buf []byte, start int) string {
	if start < 0 || start+8 > len(buf) {
		return ""
	}
	val := int64(GetInt32(buf, start)) | int64(GetInt32(buf, start+4))<<32
	whole := val / 10000
	frac := val % 10000
	if frac < 0 {
		frac = -frac
	}
	if whole < 0 {
		return fmt.Sprintf("($%d.%04d)", -whole, frac)
	}
	return fmt.Sprintf("$%d.%04d", whole, frac)
}

// NumericToString 将数值转换为字符串
func (mdb *MdbHandle) NumericToString(buf []byte, start int, scale int, prec int) string {
	if start < 0 || start+12 > len(buf) {
		return ""
	}
	_ = prec
	val := int64(GetInt32(buf, start)) | int64(GetInt32(buf, start+4))<<32
	sign := int8(buf[start+8])
	_ = sign
	result := fmt.Sprintf("%d", val)
	if scale > 0 && len(result) > scale {
		result = result[:len(result)-scale] + "." + result[len(result)-scale:]
	}
	return result
}
