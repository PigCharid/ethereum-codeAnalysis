package rlp_test

import (
	"fmt"
	"io"

	"github.com/ethereum/go-ethereum/rlp"
)

type MyCoolType struct {
	Name string
	a, b uint
}

// EncodeRLP writes x as RLP list [a, b] that omits the Name field.
// 编码器RLP将x写入RLP列表[a，b]，该列表省略了名称字段。
// 实现接口方法
func (x *MyCoolType) EncodeRLP(w io.Writer) (err error) {
	return rlp.Encode(w, []uint{x.a, x.b})
}

func ExampleEncoder() {
	var t *MyCoolType // t is nil pointer to MyCoolType t是mycoltype的空指针
	bytes, _ := rlp.EncodeToBytes(t)
	fmt.Printf("%v → %X\n", t, bytes)

	t = &MyCoolType{Name: "foobar", a: 2, b: 6}
	bytes, _ = rlp.EncodeToBytes(t)
	fmt.Printf("%v → %X\n", t, bytes)

	// 该测试案例的输出结果
	// Output:
	// <nil> → C0
	// &{foobar 5 6} → C20506
}
