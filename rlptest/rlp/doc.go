/*
Package rlp implements the RLP serialization format.
    rlp包实现了RLP序列化格式

The purpose of RLP (Recursive Linear Prefix) is to encode arbitrarily nested arrays of
binary data, and RLP is the main encoding method used to serialize objects in Ethereum.
The only purpose of RLP is to encode structure; encoding specific atomic data types (eg.
strings, ints, floats) is left up to higher-order protocols. In Ethereum integers must be
represented in big endian binary form with no leading zeroes (thus making the integer
value zero equivalent to the empty string).
    递归线性前缀（RLP）的目的是对二进制数据的任意嵌套数组进行编码，RLP是用于序列化以太坊中对象的主要编码方法。
    RLP的唯一目的是对结构进行编码；编码特定的原子数据类型（例如字符串、整数、浮点）由高阶协议决定。
    在以太坊中，整数必须以无前导零的大端二进制形式表示（从而使整数值零等效于空字符串）。

RLP values are distinguished by a type tag. The type tag precedes the value in the input
stream and defines the size and kind of the bytes that follow.
    RLP值由类型标记区分。类型标记位于输入流中的值之前，并定义后面字节的大小和类型。

Encoding Rules
    编码规则

Package rlp uses reflection and encodes RLP based on the Go type of the value.
    包rlp使用反射，并根据值的Go类型对rlp进行编码。

If the type implements the Encoder interface, Encode calls EncodeRLP. It does not
call EncodeRLP on nil pointer values.
    如果类型实现编码器接口，Encode调用EncodeRLP。它不在零指针值上调用EncodeRLP。

To encode a pointer, the value being pointed to is encoded. A nil pointer to a struct
type, slice or array always encodes as an empty RLP list unless the slice or array has
element type byte. A nil pointer to any other value encodes as the empty string.
    要对指针进行编码，需要对指向的值进行编码。指向结构类型、切片或数组的nil指针始终编码为空RLP列表，
    除非切片或数组具有元素类型byte。指向任何其他值的nil指针编码为空字符串。

Struct values are encoded as an RLP list of all their encoded public fields. Recursive
struct types are supported.
    结构值编码为其所有编码公共字段的RLP列表。支持递归结构类型。

To encode slices and arrays, the elements are encoded as an RLP list of the value's
elements. Note that arrays and slices with element type uint8 or byte are always encoded
as an RLP string.
    为了对切片和数组进行编码，元素被编码为值元素的RLP列表。注意，元素类型为uint8或byte的数组和切片始终编码为RLP字符串。

A Go string is encoded as an RLP string.
    Go字符串编码为RLP字符串。

An unsigned integer value is encoded as an RLP string. Zero always encodes as an empty RLP
string. big.Int values are treated as integers. Signed integers (int, int8, int16, ...)
are not supported and will return an error when encoding.
    无符号整数值编码为RLP字符串。零始终编码为空RLP字符串。大的Int值被视为整数。不支持有符号整数（int、int8、int16等），编码时将返回错误。

Boolean values are encoded as the unsigned integers zero (false) and one (true).
    布尔值编码为无符号整数零（false）和一（true）。

An interface value encodes as the value contained in the interface.
    接口值编码为接口中包含的值。

Floating point numbers, maps, channels and functions are not supported.
    不支持浮点数、映射、通道和函数。

Decoding Rules
    解码规则
Decoding uses the following type-dependent rules:
    解码使用以下类型相关规则：
If the type implements the Decoder interface, DecodeRLP is called.
    如果类型实现了解码器接口，则调用DecodeRLP。

To decode into a pointer, the value will be decoded as the element type of the pointer. If
the pointer is nil, a new value of the pointer's element type is allocated. If the pointer
is non-nil, the existing value will be reused. Note that package rlp never leaves a
pointer-type struct field as nil unless one of the "nil" struct tags is present.
    要解码为指针，该值将被解码为指针的元素类型。如果指针为nil，则分配指针元素类型的新值。如果指针非nil，则将重用现有值。
    注意，包rlp从不将指针类型的结构字段保留为nil，除非存在一个“nil”结构标记。

To decode into a struct, decoding expects the input to be an RLP list. The decoded
elements of the list are assigned to each public field in the order given by the struct's
definition. The input list must contain an element for each decoded field. Decoding
returns an error if there are too few or too many elements for the struct.
    要解码为结构，解码需要输入RLP列表。列表的解码元素按照结构定义给出的顺序分配给每个公共字段。输入列表必须包含每个解码字段的元素。
    如果结构的元素太少或太多，解码将返回错误。

To decode into a slice, the input must be a list and the resulting slice will contain the
input elements in order. For byte slices, the input must be an RLP string. Array types
decode similarly, with the additional restriction that the number of input elements (or
bytes) must match the array's defined length.
    要解码成一个切片，输入必须是一个列表，结果切片将按顺序包含输入元素。
    对于字节切片，输入必须是RLP字符串。数组类型的解码方式类似，但额外的限制是输入元素（或字节）的数量必须与数组定义的长度匹配。

To decode into a Go string, the input must be an RLP string. The input bytes are taken
as-is and will not necessarily be valid UTF-8.
    要解码为Go字符串，输入必须是RLP字符串。输入字节按原样取，不一定是有效的UTF-8。

To decode into an unsigned integer type, the input must also be an RLP string. The bytes
are interpreted as a big endian representation of the integer. If the RLP string is larger
than the bit size of the type, decoding will return an error. Decode also supports
*big.Int. There is no size limit for big integers.
    要解码为无符号整数类型，输入还必须是RLP字符串。字节被解释为整数的大端表示。如果RLP字符串大于该类型的位大小，则解码将返回错误。
    解码也支持*big.Int。大整数没有大小限制。

To decode into a boolean, the input must contain an unsigned integer of value zero (false)
or one (true).
    要解码为布尔值，输入必须包含值为零（false）或一（true）的无符号整数。

To decode into an interface value, one of these types is stored in the value:
    要解码为接口值，以下类型之一存储在值中：

	  []interface{}, for RLP lists
	  []byte, for RLP strings

Non-empty interface types are not supported when decoding.
    解码时不支持非空接口类型。
Signed integers, floating point numbers, maps, channels and functions cannot be decoded into.
    符号整数、浮点数、映射、通道和函数无法解码为。

Struct Tags

As with other encoding packages, the "-" tag ignores fields.
    与其他编码包一样，“－”标记忽略字段。

    type StructWithIgnoredField struct{
        Ignored uint `rlp:"-"`
        Field   uint
    }

Go struct values encode/decode as RLP lists. There are two ways of influencing the mapping
of fields to list elements. The "tail" tag, which may only be used on the last exported
struct field, allows slurping up any excess list elements into a slice.
    Go结构值编码/解码为RLP列表。有两种方法可以影响字段到列表元素的映射。
    “tail”标记只能在最后导出的struct字段上使用，它允许将任何多余的列表元素拖入切片。

    type StructWithTail struct{
        Field   uint
        Tail    []string `rlp:"tail"`
    }

The "optional" tag says that the field may be omitted if it is zero-valued. If this tag is
used on a struct field, all subsequent public fields must also be declared optional.
    “可选”标记表示，如果字段为零值，则可以省略该字段。如果此标记用于结构字段，则所有后续公共字段也必须声明为可选。

When encoding a struct with optional fields, the output RLP list contains all values up to
the last non-zero optional field.
    当使用可选字段编码结构时，输出RLP列表包含截至最后一个非零可选字段的所有值。

When decoding into a struct, optional fields may be omitted from the end of the input
list. For the example below, this means input lists of one, two, or three elements are
accepted.
    在解码为结构时，可以从输入列表的末尾省略可选字段。对于以下示例，这意味着接受一个、两个或三个元素的输入列表。

   type StructWithOptionalFields struct{
        Required  uint
        Optional1 uint `rlp:"optional"`
        Optional2 uint `rlp:"optional"`
   }

The "nil", "nilList" and "nilString" tags apply to pointer-typed fields only, and change
the decoding rules for the field type. For regular pointer fields without the "nil" tag,
input values must always match the required input length exactly and the decoder does not
produce nil values. When the "nil" tag is set, input values of size zero decode as a nil
pointer. This is especially useful for recursive types.
   “nil”、“nilList”和“nilString”标记仅适用于指针类型的字段，并更改字段类型的解码规则。对于没有“nil”标记的常规指针字段，
   输入值必须始终与所需的输入长度完全匹配，并且解码器不会产生nil值。当设置“nil”标记时，大小为零的输入值解码为nil指针。这对于递归类型特别有用。

    type StructWithNilField struct {
        Field *[3]byte `rlp:"nil"`
    }

In the example above, Field allows two possible input sizes. For input 0xC180 (a list
containing an empty string) Field is set to nil after decoding. For input 0xC483000000 (a
list containing a 3-byte string), Field is set to a non-nil array pointer.
    在上例中，字段允许两种可能的输入大小。对于输入0xC180（包含空字符串的列表），解码后字段设置为零。
    对于输入0xC483000000（包含3字节字符串的列表），字段设置为非零数组指针。

RLP supports two kinds of empty values: empty lists and empty strings. When using the
"nil" tag, the kind of empty value allowed for a type is chosen automatically. A field
whose Go type is a pointer to an unsigned integer, string, boolean or byte array/slice
expects an empty RLP string. Any other pointer field type encodes/decodes as an empty RLP
list.
    RLP支持两种类型的空值：空列表和空字符串。使用“nil”标记时，会自动选择类型允许的空值类型。
    Go类型为指向无符号整数、字符串、布尔值或字节数组/切片的指针的字段需要空RLP字符串。任何其他指针字段类型编码/解码为空RLP列表。

The choice of null value can be made explicit with the "nilList" and "nilString" struct
tags. Using these tags encodes/decodes a Go nil pointer value as the empty RLP value kind
defined by the tag.
    空值的选择可以通过“nilList”和“nilString”结构标记来明确。使用这些标记将归零指针值编码/解码为标记定义的空RLP值类型。
*/
package rlp
