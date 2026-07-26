package purego

// RC4Key RC4 密钥结构
type RC4Key struct {
	State [256]byte
	X     byte
	Y     byte
}

// RC4SetKey 设置 RC4 密钥
func RC4SetKey(key []byte) *RC4Key {
	k := &RC4Key{}

	// 初始化状态
	for i := 0; i < 256; i++ {
		k.State[i] = byte(i)
	}

	k.X = 0
	k.Y = 0
	index1 := 0
	index2 := 0

	// 打乱状态
	for counter := 0; counter < 256; counter++ {
		index2 = (int(key[index1]) + int(k.State[counter]) + index2) % 256
		// 交换
		k.State[counter], k.State[index2] = k.State[index2], k.State[counter]
		index1 = (index1 + 1) % len(key)
	}

	return k
}

// RC4Encrypt 使用 RC4 加密/解密数据（原地操作）
func RC4Encrypt(k *RC4Key, data []byte) {
	x := k.X
	y := k.Y
	state := k.State

	for i := 0; i < len(data); i++ {
		x = byte((int(x) + 1) % 256)
		y = byte((int(state[x]) + int(y)) % 256)
		state[x], state[y] = state[y], state[x]
		xorIndex := (int(state[x]) + int(state[y])) % 256
		data[i] ^= state[xorIndex]
	}

	k.X = x
	k.Y = y
}

// RC4 使用 RC4 加密/解密数据
func RC4(key []byte, data []byte) {
	if len(key) == 0 || len(data) == 0 {
		return
	}

	k := RC4SetKey(key)
	RC4Encrypt(k, data)
}

// RC4String 使用 RC4 加密字符串
func RC4String(key []byte, data string) string {
	if len(key) == 0 || len(data) == 0 {
		return data
	}

	result := []byte(data)
	RC4(key, result)
	return string(result)
}
