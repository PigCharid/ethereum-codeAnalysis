// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package trie

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
)

// 创建一个分支节点
func newTestFullNode(v []byte) []interface{} {
	fullNodeData := []interface{}{}
	for i := 0; i < 16; i++ {
		// 返回32个[]byte{byte(i + 1)} 串联的新切片
		k := bytes.Repeat([]byte{byte(i + 1)}, 32)
		fullNodeData = append(fullNodeData, k)
	}
	fullNodeData = append(fullNodeData, v)
	return fullNodeData
}

func TestDecodeNestedNode(t *testing.T) {
	fullNodeData := newTestFullNode([]byte("fullnode"))
	fmt.Println("创建完的全节点", fullNodeData)

	data := [][]byte{}

	// 设置16个空切片
	for i := 0; i < 16; i++ {
		data = append(data, nil)
	}

	data = append(data, []byte("subnode"))
	fmt.Println(data)
	fullNodeData[15] = data

	fmt.Println(fullNodeData...)

	buf := bytes.NewBuffer([]byte{})
	rlp.Encode(buf, fullNodeData)
	fmt.Println("buf数据", buf.Bytes())

	node, _ := decodeNode([]byte("testdecode"), buf.Bytes())

	fmt.Println(node)

}

func TestDecodeFullNodeWrongSizeChild(t *testing.T) {
	fullNodeData := newTestFullNode([]byte("wrongsizechild"))
	fullNodeData[0] = []byte("00")
	buf := bytes.NewBuffer([]byte{})
	rlp.Encode(buf, fullNodeData)

	_, err := decodeNode([]byte("testdecode"), buf.Bytes())
	if _, ok := err.(*decodeError); !ok {
		t.Fatalf("decodeNode returned wrong err: %v", err)
	}
}

func TestDecodeFullNodeWrongNestedFullNode(t *testing.T) {
	fullNodeData := newTestFullNode([]byte("fullnode"))

	data := [][]byte{}
	for i := 0; i < 16; i++ {
		data = append(data, []byte("123456"))
	}
	data = append(data, []byte("subnode"))
	fullNodeData[15] = data

	buf := bytes.NewBuffer([]byte{})
	rlp.Encode(buf, fullNodeData)

	_, err := decodeNode([]byte("testdecode"), buf.Bytes())
	if _, ok := err.(*decodeError); !ok {
		t.Fatalf("decodeNode returned wrong err: %v", err)
	}
}

func TestDecodeFullNode(t *testing.T) {
	fullNodeData := newTestFullNode([]byte("decodefullnode"))
	buf := bytes.NewBuffer([]byte{})
	rlp.Encode(buf, fullNodeData)

	node, _ := decodeNode([]byte("testdecode"), buf.Bytes())
	fmt.Println(node)
}
