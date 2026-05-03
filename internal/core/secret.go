package core

// ZeroBytes overwrites a byte slice in place.
func ZeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
