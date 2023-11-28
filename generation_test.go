package uilib

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

func TestXxx(t *testing.T) {
	v5s := uuid.NewSHA1(namespace, []byte("session"))
	v5c := uuid.NewSHA1(namespace, []byte("controller"))
	fmt.Printf("v5c: %v\n", v5c)
	fmt.Printf("v5s: %v\n", v5s)

	h := md5.Sum([]byte("isdi"))
	fmt.Printf("hex.EncodeToString(h[:]): %v\n", hex.EncodeToString(h[:]))
}
