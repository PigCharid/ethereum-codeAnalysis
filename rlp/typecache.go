package rlp

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/rlp/internal/rlpstruct"
)

// 如何根据类型找到对应的编码器和解码器

// typeinfo is an entry in the type cache.
// 缓冲类型的结构信息
type typeinfo struct {
	decoder    decoder //解码器
	decoderErr error   // error from makeDecoder
	writer     writer  //编码器
	writerErr  error   // error from makeWriter
}

// typekey is the key of a type in typeCache. It includes the struct tags because
// they might generate a different decoder.
// 类型和Tag
// typekey是typeCache中类型的键。它包括struct标记，因为它们可能生成不同的解码器。
type typekey struct {
	reflect.Type   //数据类型
	rlpstruct.Tags //根据tag可能会生成不同的解码器
}

type decoder func(*Stream, reflect.Value) error

type writer func(reflect.Value, *encBuffer) error

var theTC = newTypeCache()

// 核心数据结构  Map的key是类型，value是对应的编码和解码器
type typeCache struct {
	cur atomic.Value
	// This lock synchronizes writers.
	// 此锁同步写入程序。
	mu   sync.Mutex
	next map[typekey]*typeinfo // 类型->编码|解码函数的映射，不同的数据类型对应不同的编码和解码方法
}

func newTypeCache() *typeCache {
	c := new(typeCache)
	c.cur.Store(make(map[typekey]*typeinfo))
	return c
}

func cachedDecoder(typ reflect.Type) (decoder, error) {
	info := theTC.info(typ)
	return info.decoder, info.decoderErr
}

func cachedWriter(typ reflect.Type) (writer, error) {
	// 通过全局的Typecache对象去判断，返回一个什么样的编码器 全局的类型缓冲对象的作用，后面了解清楚
	// typecache对象里面有一个map   typekey->typeinfo   typekey就是类型和tag  typeinfo就是编码解码器
	info := theTC.info(typ)
	return info.writer, info.writerErr
}

// 返回解码编码器对象
func (c *typeCache) info(typ reflect.Type) *typeinfo {
	// 封装一个type对象
	key := typekey{Type: typ}

	// 看下缓冲的typecache对象中没有没这个typekey  对应的typeinfo 有的话就返回
	if info := c.cur.Load().(map[typekey]*typeinfo)[key]; info != nil {
		return info
	}

	// Not in the cache, need to generate info for this type.
	// 不在缓存中，需要生成此类型的信息。
	// 传入类型 和一个空的Tag对象
	return c.generate(typ, rlpstruct.Tags{})
}

func (c *typeCache) generate(typ reflect.Type, tags rlpstruct.Tags) *typeinfo {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 载入缓冲池
	cur := c.cur.Load().(map[typekey]*typeinfo)
	// 再检查是否已经有了对应的key-value
	if info := cur[typekey{typ, tags}]; info != nil {
		return info
	}

	// Copy cur to next.
	// 重新把全局的typecache赋值一遍
	c.next = make(map[typekey]*typeinfo, len(cur)+1)
	for k, v := range cur {
		c.next[k] = v
	}

	// Generate.
	info := c.infoWhileGenerating(typ, tags)

	// next -> cur
	c.cur.Store(c.next)
	c.next = nil
	return info
}

func (c *typeCache) infoWhileGenerating(typ reflect.Type, tags rlpstruct.Tags) *typeinfo {
	key := typekey{typ, tags}
	// 继续检查缓冲池
	if info := c.next[key]; info != nil {
		return info
	}
	// Put a dummy value into the cache before generating.
	// If the generator tries to lookup itself, it will get
	// the dummy value and won't call itself recursively.
	//在生成之前，将一个伪值放入缓存。如果生成器尝试查找自身，它将获得伪值，并且不会递归调用自身。
	// 创建一个空的typeinfo
	info := new(typeinfo)
	// 存入map
	c.next[key] = info

	// 创建编码解码器
	info.generate(typ, tags)
	return info
}

type field struct {
	index    int
	info     *typeinfo
	optional bool
}

// structFields resolves the typeinfo of all public fields in a struct type.
func structFields(typ reflect.Type) (fields []field, err error) {
	// Convert fields to rlpstruct.Field.
	var allStructFields []rlpstruct.Field
	for i := 0; i < typ.NumField(); i++ {
		rf := typ.Field(i)
		allStructFields = append(allStructFields, rlpstruct.Field{
			Name:     rf.Name,
			Index:    i,
			Exported: rf.PkgPath == "",
			Tag:      string(rf.Tag),
			Type:     *rtypeToStructType(rf.Type, nil),
		})
	}

	// Filter/validate fields.
	structFields, structTags, err := rlpstruct.ProcessFields(allStructFields)
	if err != nil {
		if tagErr, ok := err.(rlpstruct.TagError); ok {
			tagErr.StructType = typ.String()
			return nil, tagErr
		}
		return nil, err
	}

	// Resolve typeinfo.
	for i, sf := range structFields {
		typ := typ.Field(sf.Index).Type
		tags := structTags[i]
		info := theTC.infoWhileGenerating(typ, tags)
		fields = append(fields, field{sf.Index, info, tags.Optional})
	}
	return fields, nil
}

// firstOptionalField returns the index of the first field with "optional" tag.
func firstOptionalField(fields []field) int {
	for i, f := range fields {
		if f.optional {
			return i
		}
	}
	return len(fields)
}

type structFieldError struct {
	typ   reflect.Type
	field int
	err   error
}

func (e structFieldError) Error() string {
	return fmt.Sprintf("%v (struct field %v.%s)", e.err, e.typ, e.typ.Field(e.field).Name)
}

func (i *typeinfo) generate(typ reflect.Type, tags rlpstruct.Tags) {
	// 创建解码器
	i.decoder, i.decoderErr = makeDecoder(typ, tags)
	// 创建编码器
	i.writer, i.writerErr = makeWriter(typ, tags)
}

// rtypeToStructType converts typ to rlpstruct.Type.
func rtypeToStructType(typ reflect.Type, rec map[reflect.Type]*rlpstruct.Type) *rlpstruct.Type {
	k := typ.Kind()
	if k == reflect.Invalid {
		panic("invalid kind")
	}

	if prev := rec[typ]; prev != nil {
		return prev // short-circuit for recursive types
	}
	if rec == nil {
		rec = make(map[reflect.Type]*rlpstruct.Type)
	}

	t := &rlpstruct.Type{
		Name:      typ.String(),
		Kind:      k,
		IsEncoder: typ.Implements(encoderInterface),
		IsDecoder: typ.Implements(decoderInterface),
	}
	rec[typ] = t
	if k == reflect.Array || k == reflect.Slice || k == reflect.Ptr {
		t.Elem = rtypeToStructType(typ.Elem(), rec)
	}
	return t
}

// typeNilKind gives the RLP value kind for nil pointers to 'typ'.
func typeNilKind(typ reflect.Type, tags rlpstruct.Tags) Kind {
	styp := rtypeToStructType(typ, nil)

	var nk rlpstruct.NilKind
	if tags.NilOK {
		nk = tags.NilKind
	} else {
		nk = styp.DefaultNilValue()
	}
	switch nk {
	case rlpstruct.NilKindString:
		return String
	case rlpstruct.NilKindList:
		return List
	default:
		panic("invalid nil kind value")
	}
}

func isUint(k reflect.Kind) bool {
	return k >= reflect.Uint && k <= reflect.Uintptr
}

func isByte(typ reflect.Type) bool {
	return typ.Kind() == reflect.Uint8 && !typ.Implements(encoderInterface)
}
