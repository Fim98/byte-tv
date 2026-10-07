package app

import (
	"errors"
)

func pkcs7Unpad(src []byte, blockSize int) ([]byte, error) {
	if len(src) == 0 || len(src)%blockSize != 0 {
		return nil, errors.New("invalid pkcs7 length")
	}
	pad := int(src[len(src)-1])
	if pad == 0 || pad > blockSize || pad > len(src) {
		return nil, errors.New("invalid pkcs7 padding")
	}
	for _, v := range src[len(src)-pad:] {
		if int(v) != pad {
			return nil, errors.New("invalid pkcs7 padding bytes")
		}
	}
	return src[:len(src)-pad], nil
}
