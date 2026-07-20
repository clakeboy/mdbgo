package mdbgo

import (
	"fmt"
	"testing"
)

func TestConvertProperty(t *testing.T) {
	// 测试转换属性
	fmt.Println(PtToPixel(9))
	fmt.Println(ToRGBHex(-2147483633))
	fmt.Println(ToRGBHex(-2147483630))
}

func PtToPixel(num int) int {
	return int(float64(num) * 96 / 72)
}

func ToRGBHex(color int) string {
	redHex := fmt.Sprintf("%02x", (color>>16)&0xFF)  // 获取Red分量并确保是2位16进制数
	greenHex := fmt.Sprintf("%02x", (color>>8)&0xFF) // 获取Green分量并确保是2位16进制数
	blueHex := fmt.Sprintf("%02x", color&0xFF)       // 获取Blue分量并确保是2位16进制数
	return "#" + blueHex + greenHex + redHex
}
