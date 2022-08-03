package trie

// Trie keys are dealt with in three distinct encodings:
//Trie键以三种不同的编码处理：

// KEYBYTES encoding contains the actual key and nothing else. This encoding is the
// input to most API functions.
// KEYBYTES编码包含实际密钥，而不包含其他内容。这种编码是大多数API函数的输入。

// HEX encoding contains one byte for each nibble of the key and an optional trailing
// 'terminator' byte of value 0x10 which indicates whether or not the node at the key
// contains a value. Hex key encoding is used for nodes loaded in memory because it's
// convenient to access.
//十六进制编码为键的每个半字节包含一个字节，以及值0x10的可选尾部“终止符”字节，该字节指示键处的节点是否包含值。十六进制编码用于加载到内存中的节点，因为它便于访问。

// COMPACT encoding is defined by the Ethereum Yellow Paper (it's called "hex prefix
// encoding" there) and contains the bytes of the key and a flag. The high nibble of the
// first byte contains the flag; the lowest bit encoding the oddness of the length and
// the second-lowest encoding whether the node at the key is a value node. The low nibble
// of the first byte is zero in the case of an even number of nibbles and the first nibble
// in the case of an odd number. All remaining nibbles (now an even number) fit properly
// into the remaining bytes. Compact encoding is used for nodes stored on disk.
//紧凑编码由以太坊黄皮定义（在那里称为“十六进制前缀编码”），包含密钥字节和标志。第一个字节的高位半字节包含标志；最低位编码长度的奇异性，第二低位编码键处的节点是否为值节点。
//对于偶数个半字节，第一个字节的低半字节为零；对于奇数，第一个半字节为0。所有剩余的半字节（现在是偶数）都正确地放入剩余的字节中。紧凑编码用于存储在磁盘上的节点。

// compact编码主要是吧内存的数据和数据库的数据进行转化    也叫Hex prefix编码（HP） 基于Hex编码

// Hex编码 Hex编码：当[key， value]数据插入MPT时，这里的key必须经过特殊编码以保证能以16进制形式按位进入fullNode.Children[]。
// 由于Children数组最多容纳16个字节点，所以以太坊这里定义了Hex编码方式将1bytes的字符大小限制在4bit(16进制)以内。trie给出的Hex编码方式如下：

// hex编码转化成Compact编码
func hexToCompact(hex []byte) []byte {
	// 如果最后一位是16，terminator为1，否则为0
	terminator := byte(0)
	// 检查结尾是否有0x10 => 16  包含terminator的节点为叶子节点
	if hasTerm(hex) {
		// 标记为叶子节点
		terminator = 1
		// 去除Hex尾部标记
		hex = hex[:len(hex)-1]
	}
	// 定义Compat字节数组
	buf := make([]byte, len(hex)/2+1)

	// 标志byte为00000000或者0010000  就是判断是否为叶子节点   标志位默认

	buf[0] = terminator << 5 // the flag byte

	// 如果长度为奇数，添加奇数位标志1，并把第一个nibble字节放入buf[0]的低四位
	// 位运算   都为一才为1ni
	if len(hex)&1 == 1 {
		// 如果Hex长度为奇数，修改标志位为odd flag
		buf[0] |= 1 << 4 // odd flag
		// 然后把第1个nibble放入buf[0]低四位
		buf[0] |= hex[0] // first nibble is contained in the first byte
		hex = hex[1:]
	}

	//将两个nibble字节合并成一个字节   然后将每2nibble的数据合并到1个byte
	decodeNibbles(hex, buf[1:])
	return buf
}

// hexToCompactInPlace places the compact key in input buffer, returning the length
// needed for the representation
// hexToCompactInPlace将压缩键放在输入缓冲区中，返回表示所需的长度
func hexToCompactInPlace(hex []byte) int {
	var (
		hexLen    = len(hex) // length of the hex input
		firstByte = byte(0)
	)
	// Check if we have a terminator there
	if hexLen > 0 && hex[hexLen-1] == 16 {
		firstByte = 1 << 5
		hexLen-- // last part was the terminator, ignore that
	}
	var (
		binLen = hexLen/2 + 1
		ni     = 0 // index in hex
		bi     = 1 // index in bin (compact)
	)
	if hexLen&1 == 1 {
		firstByte |= 1 << 4 // odd flag
		firstByte |= hex[0] // first nibble is contained in the first byte
		ni++
	}
	for ; ni < hexLen; bi, ni = bi+1, ni+2 {
		hex[bi] = hex[ni]<<4 | hex[ni+1]
	}
	hex[0] = firstByte
	return binLen
}

// compact编码转hex编码
func compactToHex(compact []byte) []byte {
	if len(compact) == 0 {
		return compact
	}

	base := keybytesToHex(compact)
	// delete terminator flag
	/*这里base[0]有4中情况
	  00000000	扩展节点偶数位
	  00000001	扩展节点奇数位
	  00000010	叶子节点偶数位
	  00000011	叶子节点偶数位
	*/
	if base[0] < 2 {
		// 如果是扩展节点，去除最后一位
		base = base[:len(base)-1]
	}
	// apply odd flag
	// 如果是偶数位chop=2，否则chop=1
	chop := 2 - base[0]&1
	//去除compact标志位。偶数位去除2个字节，奇数位去除1个字节（因为奇数位的低四位放的是nibble数据）
	return base[chop:]
}

// 将keybytes 转成hex
func keybytesToHex(str []byte) []byte {
	//hex编码的长度
	l := len(str)*2 + 1
	//将一个keybyte转化成两个字节

	var nibbles = make([]byte, l)
	// 把源数据的每一位都分成两位存储，源数据位/16=第一位 源数据位%16=第二位
	for i, b := range str {
		nibbles[i*2] = b / 16
		nibbles[i*2+1] = b % 16
	}
	//末尾加入Hex标志位16 00010000
	nibbles[l-1] = 16
	return nibbles
}

// hexToKeybytes turns hex nibbles into key bytes.
// This can only be used for keys of even length.
// hex转换成keybytes
func hexToKeybytes(hex []byte) []byte {
	// 看下最后一位是不是16
	if hasTerm(hex) {
		hex = hex[:len(hex)-1]
	}
	// 去掉最后标志位的hex编码的长度不会为奇数
	if len(hex)&1 != 0 {
		panic("can't convert hex key of odd length")
	}
	// 申明keybytes
	key := make([]byte, len(hex)/2)
	// keybytes转化成hex的逆操作
	decodeNibbles(hex, key)
	return key
}

func decodeNibbles(nibbles []byte, bytes []byte) {

	for bi, ni := 0, 0; ni < len(nibbles); bi, ni = bi+1, ni+2 {
		bytes[bi] = nibbles[ni]<<4 | nibbles[ni+1]
	}
}

// prefixLen returns the length of the common prefix of a and b.
// 相同前缀的长度
func prefixLen(a, b []byte) int {
	var i, length = 0, len(a)
	if len(b) < length {
		length = len(b)
	}
	for ; i < length; i++ {
		if a[i] != b[i] {
			break
		}
	}
	return i
}

// hasTerm returns whether a hex key has the terminator flag.
// 结尾是否有terminator标志
func hasTerm(s []byte) bool {
	return len(s) > 0 && s[len(s)-1] == 16
}
