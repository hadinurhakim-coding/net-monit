package main

import (
	"encoding/json"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func newBlob(d []byte) *windows.DataBlob {
	if len(d) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(d)), Data: &d[0]}
}

// encryptBytes encrypts data with Windows DPAPI, tying it to the current user account.
// An attacker with a different Windows account cannot decrypt the stored values.
func encryptBytes(data []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptProtectData(newBlob(data), nil, nil, 0, nil, 0, &out); err != nil {
		return nil, fmt.Errorf("DPAPI encrypt: %w", err)
	}
	result := make([]byte, out.Size)
	copy(result, unsafe.Slice(out.Data, out.Size))
	_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return result, nil
}

// decryptBytes reverses encryptBytes.
func decryptBytes(data []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(newBlob(data), nil, nil, 0, nil, 0, &out); err != nil {
		return nil, fmt.Errorf("DPAPI decrypt: %w", err)
	}
	result := make([]byte, out.Size)
	copy(result, unsafe.Slice(out.Data, out.Size))
	_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return result, nil
}

// unmarshalDecrypt tries DPAPI-decrypt then JSON unmarshal.
// Falls back to direct unmarshal so existing unencrypted records remain readable.
func unmarshalDecrypt(v []byte, dst any) bool {
	if plain, err := decryptBytes(v); err == nil {
		if json.Unmarshal(plain, dst) == nil {
			return true
		}
	}
	return json.Unmarshal(v, dst) == nil
}
