package main

import (
	"fmt"
	"io"

	"rlptest/rlp"
)

// 测试结构体
type MyCoolType struct {
	Name string
	a, b uint
}

// 实现Encoder接口
func (x *MyCoolType) EncodeRLP(w io.Writer) (err error) {
	return rlp.Encode(w, []uint{x.a, x.b})
}

func main() {
	// var t *MyCoolType // t is nil pointer to MyCoolType t是mycoltype的空指针
	// bytes, _ := rlp.EncodeToBytes(t)
	// fmt.Printf("%v → %X\n", t, bytes)

	// t := &MyCoolType{Name: "foobar", a: 2, b: 6}
	// 调用EncodeToBytes()
	t := "Aaa"
	bytes, _ := rlp.EncodeToBytes(t)

	fmt.Printf("%v → %X\n", t, bytes)
}
