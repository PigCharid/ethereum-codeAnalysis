package rlp

import (
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"

	"github.com/ethereum/go-ethereum/rlp/internal/rlpstruct"
)

var (
	//通用编码值。
	//这些在实现EncodeRLP时很有用
	EmptyString = []byte{0x80} //空字符串的编码值
	EmptyList   = []byte{0xC0} //空列表的编码值
)

// rlp：无法编码负大整数
var ErrNegativeBigInt = errors.New("rlp: cannot encode negative big.Int")

// Encoder is implemented by types that require custom
// encoding rules or want to encode private fields.
// 编码器由需要自定义编码规则或希望编码私有字段的类型实现。
type Encoder interface {
	// EncodeRLP should write the RLP encoding of its receiver to w.
	// If the implementation is a pointer method, it may also be
	// called for nil pointers.
	//
	// Implementations should generate valid RLP. The data written is
	// not verified at the moment, but a future version might. It is
	// recommended to write only a single value but writing multiple
	// values or no value at all is also permitted.
	// 编码器RLP应该将其接收器的RLP编码写入w。如果实现是指针方法，也可以为零指针调用它。
	// 实现应生成有效的RLP。写入的数据目前尚未得到验证，但未来的版本可能会得到验证。建议只写入一个值，但也允许写入多个值或根本不写入值。
	EncodeRLP(io.Writer) error
}

// Encode writes the RLP encoding of val to w. Note that Encode may
// perform many small writes in some cases. Consider making w
// buffered.
//
// Please see package-level documentation of encoding rules.
func Encode(w io.Writer, val interface{}) error {
	// Optimization: reuse *encBuffer when called by EncodeRLP.
	if buf := encBufferFromWriter(w); buf != nil {
		return buf.encode(val)
	}

	buf := getEncBuffer()
	defer encBufferPool.Put(buf)
	if err := buf.encode(val); err != nil {
		return err
	}
	return buf.writeTo(w)
}

// EncodeToBytes returns the RLP encoding of val.
// EncodeToBytes返回val的RLP编码。
// Please see package-level documentation for the encoding rules.
// 有关编码规则，请参阅包级文档。
func EncodeToBytes(val interface{}) ([]byte, error) {
	//从encbufPool池中获取encbuf实例  这个地方目前还没看懂  暂时先研究编码
	buf := getEncBuffer()
	// 调用结束以后重新放入池中
	defer encBufferPool.Put(buf)

	// 对数据编码
	if err := buf.encode(val); err != nil {
		return nil, err
	}
	//将编码后的数据和头部拼接成byte[]后返回
	return buf.makeBytes(), nil
}

// EncodeToReader returns a reader from which the RLP encoding of val
// can be read. The returned size is the total size of the encoded
// data.
//
// Please see the documentation of Encode for the encoding rules.
func EncodeToReader(val interface{}) (size int, r io.Reader, err error) {
	buf := getEncBuffer()
	if err := buf.encode(val); err != nil {
		encBufferPool.Put(buf)
		return 0, nil, err
	}
	// Note: can't put the reader back into the pool here
	// because it is held by encReader. The reader puts it
	// back when it has been fully consumed.
	return buf.size(), &encReader{buf: buf}, nil
}

type listhead struct {
	offset int // index of this header in string data // 记录了列表数据在str字段的起始位置
	size   int // total size of encoded data (including list headers) //// 编码数据的总长度 (包括列表头)
}

// encode writes head to the given buffer, which must be at least
// 9 bytes long. It returns the encoded bytes.
func (head *listhead) encode(buf []byte) []byte {
	return buf[:puthead(buf, 0xC0, 0xF7, uint64(head.size))]
}

// headsize returns the size of a list or string header
// for a value of the given size.
func headsize(size uint64) int {
	if size < 56 {
		return 1
	}
	return 1 + intsize(size)
}

// puthead writes a list or string header to buf.
// buf must be at least 9 bytes long.
func puthead(buf []byte, smalltag, largetag byte, size uint64) int {
	if size < 56 {
		buf[0] = smalltag + byte(size)
		return 1
	}
	sizesize := putint(buf[1:], size)
	buf[0] = largetag + byte(sizesize)
	return sizesize + 1
}

var encoderInterface = reflect.TypeOf(new(Encoder)).Elem()

// makeWriter creates a writer function for the given type.
// 通过所给的type和tag创建编码器
func makeWriter(typ reflect.Type, ts rlpstruct.Tags) (writer, error) {
	// 更加细致的分类
	kind := typ.Kind()
	switch {
	case typ == rawValueType:
		return writeRawValue, nil

	case typ.AssignableTo(reflect.PtrTo(bigInt)):
		return writeBigIntPtr, nil

	case typ.AssignableTo(bigInt):
		return writeBigIntNoPtr, nil

	case kind == reflect.Ptr:
		return makePtrWriter(typ, ts)

	// 实现了encoder接口的类型
	case reflect.PtrTo(typ).Implements(encoderInterface):
		return makeEncoderWriter(typ), nil

	case isUint(kind):
		return writeUint, nil

	case kind == reflect.Bool:
		return writeBool, nil

	case kind == reflect.String:
		return writeString, nil

	case kind == reflect.Slice && isByte(typ.Elem()):
		return writeBytes, nil

	case kind == reflect.Array && isByte(typ.Elem()):
		return makeByteArrayWriter(typ), nil

	case kind == reflect.Slice || kind == reflect.Array:
		return makeSliceWriter(typ, ts)

	case kind == reflect.Struct:
		return makeStructWriter(typ)

	case kind == reflect.Interface:
		return writeInterface, nil

	default:
		return nil, fmt.Errorf("rlp: type %v is not RLP-serializable", typ)
	}
}

func writeRawValue(val reflect.Value, w *encBuffer) error {
	w.str = append(w.str, val.Bytes()...)
	return nil
}

func writeUint(val reflect.Value, w *encBuffer) error {
	w.writeUint64(val.Uint())
	return nil
}

func writeBool(val reflect.Value, w *encBuffer) error {
	w.writeBool(val.Bool())
	return nil
}

func writeBigIntPtr(val reflect.Value, w *encBuffer) error {
	ptr := val.Interface().(*big.Int)
	if ptr == nil {
		w.str = append(w.str, 0x80)
		return nil
	}
	if ptr.Sign() == -1 {
		return ErrNegativeBigInt
	}
	w.writeBigInt(ptr)
	return nil
}

func writeBigIntNoPtr(val reflect.Value, w *encBuffer) error {
	i := val.Interface().(big.Int)
	if i.Sign() == -1 {
		return ErrNegativeBigInt
	}
	w.writeBigInt(&i)
	return nil
}

func writeBytes(val reflect.Value, w *encBuffer) error {
	w.writeBytes(val.Bytes())
	return nil
}

func makeByteArrayWriter(typ reflect.Type) writer {
	switch typ.Len() {
	case 0:
		return writeLengthZeroByteArray
	case 1:
		return writeLengthOneByteArray
	default:
		length := typ.Len()
		return func(val reflect.Value, w *encBuffer) error {
			if !val.CanAddr() {
				// Getting the byte slice of val requires it to be addressable. Make it
				// addressable by copying.
				copy := reflect.New(val.Type()).Elem()
				copy.Set(val)
				val = copy
			}
			slice := byteArrayBytes(val, length)
			w.encodeStringHeader(len(slice))
			w.str = append(w.str, slice...)
			return nil
		}
	}
}

func writeLengthZeroByteArray(val reflect.Value, w *encBuffer) error {
	w.str = append(w.str, 0x80)
	return nil
}

func writeLengthOneByteArray(val reflect.Value, w *encBuffer) error {
	b := byte(val.Index(0).Uint())
	if b <= 0x7f {
		w.str = append(w.str, b)
	} else {
		w.str = append(w.str, 0x81, b)
	}
	return nil
}

func writeString(val reflect.Value, w *encBuffer) error {
	s := val.String()
	if len(s) == 1 && s[0] <= 0x7f {
		// fits single byte, no string header
		w.str = append(w.str, s[0])
	} else {
		w.encodeStringHeader(len(s))
		w.str = append(w.str, s...)
	}
	return nil
}

func writeInterface(val reflect.Value, w *encBuffer) error {
	if val.IsNil() {
		// Write empty list. This is consistent with the previous RLP
		// encoder that we had and should therefore avoid any
		// problems.
		w.str = append(w.str, 0xC0)
		return nil
	}
	eval := val.Elem()
	writer, err := cachedWriter(eval.Type())
	if err != nil {
		return err
	}
	return writer(eval, w)
}

func makeSliceWriter(typ reflect.Type, ts rlpstruct.Tags) (writer, error) {
	etypeinfo := theTC.infoWhileGenerating(typ.Elem(), rlpstruct.Tags{})
	if etypeinfo.writerErr != nil {
		return nil, etypeinfo.writerErr
	}

	var wfn writer
	if ts.Tail {
		// This is for struct tail slices.
		// w.list is not called for them.
		wfn = func(val reflect.Value, w *encBuffer) error {
			vlen := val.Len()
			for i := 0; i < vlen; i++ {
				if err := etypeinfo.writer(val.Index(i), w); err != nil {
					return err
				}
			}
			return nil
		}
	} else {
		// This is for regular slices and arrays.
		wfn = func(val reflect.Value, w *encBuffer) error {
			vlen := val.Len()
			if vlen == 0 {
				w.str = append(w.str, 0xC0)
				return nil
			}
			listOffset := w.list()
			for i := 0; i < vlen; i++ {
				if err := etypeinfo.writer(val.Index(i), w); err != nil {
					return err
				}
			}
			w.listEnd(listOffset)
			return nil
		}
	}
	return wfn, nil
}

func makeStructWriter(typ reflect.Type) (writer, error) {
	fields, err := structFields(typ)
	if err != nil {
		return nil, err
	}
	for _, f := range fields {
		if f.info.writerErr != nil {
			return nil, structFieldError{typ, f.index, f.info.writerErr}
		}
	}

	var writer writer
	firstOptionalField := firstOptionalField(fields)
	if firstOptionalField == len(fields) {
		// This is the writer function for structs without any optional fields.
		writer = func(val reflect.Value, w *encBuffer) error {
			lh := w.list()
			for _, f := range fields {
				if err := f.info.writer(val.Field(f.index), w); err != nil {
					return err
				}
			}
			w.listEnd(lh)
			return nil
		}
	} else {
		// If there are any "optional" fields, the writer needs to perform additional
		// checks to determine the output list length.
		writer = func(val reflect.Value, w *encBuffer) error {
			lastField := len(fields) - 1
			for ; lastField >= firstOptionalField; lastField-- {
				if !val.Field(fields[lastField].index).IsZero() {
					break
				}
			}
			lh := w.list()
			for i := 0; i <= lastField; i++ {
				if err := fields[i].info.writer(val.Field(fields[i].index), w); err != nil {
					return err
				}
			}
			w.listEnd(lh)
			return nil
		}
	}
	return writer, nil
}

func makePtrWriter(typ reflect.Type, ts rlpstruct.Tags) (writer, error) {
	nilEncoding := byte(0xC0)
	if typeNilKind(typ.Elem(), ts) == String {
		nilEncoding = 0x80
	}

	etypeinfo := theTC.infoWhileGenerating(typ.Elem(), rlpstruct.Tags{})
	if etypeinfo.writerErr != nil {
		return nil, etypeinfo.writerErr
	}

	writer := func(val reflect.Value, w *encBuffer) error {
		if ev := val.Elem(); ev.IsValid() {
			return etypeinfo.writer(ev, w)
		}
		w.str = append(w.str, nilEncoding)
		return nil
	}
	return writer, nil
}

func makeEncoderWriter(typ reflect.Type) writer {
	// 如果是实现encode接口类型
	if typ.Implements(encoderInterface) {
		// 返回一个和writer编码器一样的类型函数
		return func(val reflect.Value, w *encBuffer) error {
			return val.Interface().(Encoder).EncodeRLP(w)
		}
	}
	w := func(val reflect.Value, w *encBuffer) error {
		if !val.CanAddr() {
			// package json simply doesn't call MarshalJSON for this case, but encodes the
			// value as if it didn't implement the interface. We don't want to handle it that
			// way.
			return fmt.Errorf("rlp: unadressable value of type %v, EncodeRLP is pointer method", val.Type())
		}
		return val.Addr().Interface().(Encoder).EncodeRLP(w)
	}
	return w
}

// putint writes i to the beginning of b in big endian byte
// order, using the least number of bytes needed to represent i.
func putint(b []byte, i uint64) (size int) {
	switch {
	case i < (1 << 8):
		b[0] = byte(i)
		return 1
	case i < (1 << 16):
		b[0] = byte(i >> 8)
		b[1] = byte(i)
		return 2
	case i < (1 << 24):
		b[0] = byte(i >> 16)
		b[1] = byte(i >> 8)
		b[2] = byte(i)
		return 3
	case i < (1 << 32):
		b[0] = byte(i >> 24)
		b[1] = byte(i >> 16)
		b[2] = byte(i >> 8)
		b[3] = byte(i)
		return 4
	case i < (1 << 40):
		b[0] = byte(i >> 32)
		b[1] = byte(i >> 24)
		b[2] = byte(i >> 16)
		b[3] = byte(i >> 8)
		b[4] = byte(i)
		return 5
	case i < (1 << 48):
		b[0] = byte(i >> 40)
		b[1] = byte(i >> 32)
		b[2] = byte(i >> 24)
		b[3] = byte(i >> 16)
		b[4] = byte(i >> 8)
		b[5] = byte(i)
		return 6
	case i < (1 << 56):
		b[0] = byte(i >> 48)
		b[1] = byte(i >> 40)
		b[2] = byte(i >> 32)
		b[3] = byte(i >> 24)
		b[4] = byte(i >> 16)
		b[5] = byte(i >> 8)
		b[6] = byte(i)
		return 7
	default:
		b[0] = byte(i >> 56)
		b[1] = byte(i >> 48)
		b[2] = byte(i >> 40)
		b[3] = byte(i >> 32)
		b[4] = byte(i >> 24)
		b[5] = byte(i >> 16)
		b[6] = byte(i >> 8)
		b[7] = byte(i)
		return 8
	}
}

// intsize computes the minimum number of bytes required to store i.
func intsize(i uint64) (size int) {
	for size = 1; ; size++ {
		if i >>= 8; i == 0 {
			return size
		}
	}
}
