package sender

// fnv32aBytes 计算 payload 的 FNV-1a 32bit 哈希。
// 相比 hash/fnv.New32a，可避免接口调用与对象创建开销。
func fnv32aBytes(payload []byte) uint32 {
	h := uint32(2166136261)
	for _, b := range payload {
		h ^= uint32(b)
		h *= 16777619
	}
	return h
}

func fnv32aString(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}
