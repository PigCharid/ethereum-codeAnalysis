package trie

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/rand"
	"testing"
)

func TestHexCompact(t *testing.T) {
	hex := []byte{0, 1, 0, 2, 0, 3, 0, 4, 16}
	compact := hexToCompact(hex)
	fmt.Println(compact)
}

func TestHexKeybytes(t *testing.T) {
	keybytes := []byte{1, 2, 3, 4}
	hex := keybytesToHex(keybytes)
	fmt.Println(hex)
}

func TestHexToCompactInPlace(t *testing.T) {
	for i, keyS := range []string{
		"00",
		"060a040c0f000a090b040803010801010900080d090a0a0d0903000b10",
		"10",
	} {
		hexBytes, _ := hex.DecodeString(keyS)
		exp := hexToCompact(hexBytes)
		sz := hexToCompactInPlace(hexBytes)
		got := hexBytes[:sz]
		if !bytes.Equal(exp, got) {
			t.Fatalf("test %d: encoding err\ninp %v\ngot %x\nexp %x\n", i, keyS, got, exp)
		}
	}
}

func TestHexToCompactInPlaceRandom(t *testing.T) {
	for i := 0; i < 10000; i++ {
		l := rand.Intn(128)
		key := make([]byte, l)
		rand.Read(key)
		hexBytes := keybytesToHex(key)
		hexOrig := []byte(string(hexBytes))
		exp := hexToCompact(hexBytes)
		sz := hexToCompactInPlace(hexBytes)
		got := hexBytes[:sz]

		if !bytes.Equal(exp, got) {
			t.Fatalf("encoding err \ncpt %x\nhex %x\ngot %x\nexp %x\n",
				key, hexOrig, got, exp)
		}
	}
}

func BenchmarkHexToCompact(b *testing.B) {
	testBytes := []byte{0, 15, 1, 12, 11, 8, 16 /*term*/}
	for i := 0; i < b.N; i++ {
		hexToCompact(testBytes)
	}
}

func BenchmarkCompactToHex(b *testing.B) {
	testBytes := []byte{0, 15, 1, 12, 11, 8, 16 /*term*/}
	for i := 0; i < b.N; i++ {
		compactToHex(testBytes)
	}
}

func BenchmarkKeybytesToHex(b *testing.B) {
	testBytes := []byte{7, 6, 6, 5, 7, 2, 6, 2, 16}
	for i := 0; i < b.N; i++ {
		keybytesToHex(testBytes)
	}
}

func BenchmarkHexToKeybytes(b *testing.B) {
	testBytes := []byte{7, 6, 6, 5, 7, 2, 6, 2, 16}
	for i := 0; i < b.N; i++ {
		hexToKeybytes(testBytes)
	}
}
