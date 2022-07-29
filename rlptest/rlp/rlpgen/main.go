package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/types"
	"os"

	"golang.org/x/tools/go/packages"
)

const pathOfPackageRLP = "github.com/ethereum/go-ethereum/rlp"

type Config struct {
	Dir             string // input package directory
	Type            string
	GenerateEncoder bool
	GenerateDecoder bool
}

func main() {
	// 获取配置参数
	var (
		pkgdir     = flag.String("dir", ".", "input package")
		output     = flag.String("out", "-", "output file (default is stdout)")
		genEncoder = flag.Bool("encoder", true, "generate EncodeRLP?")
		genDecoder = flag.Bool("decoder", false, "generate DecodeRLP?")
		typename   = flag.String("type", "", "type to generate methods for")
	)
	flag.Parse()

	// 配置对象
	cfg := Config{
		Dir:             *pkgdir,
		Type:            *typename,
		GenerateEncoder: *genEncoder,
		GenerateDecoder: *genDecoder,
	}

	//
	code, err := cfg.process()

	if err != nil {
		fatal(err)
	}
	if *output == "-" {
		os.Stdout.Write(code)
	} else if err := os.WriteFile(*output, code, 0600); err != nil {
		fatal(err)
	}
}

func fatal(args ...interface{}) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}

// process generates the Go code.
// 进程生成Go代码
func (cfg *Config) process() (code []byte, err error) {
	// Load packages.
	// 加载包
	pcfg := &packages.Config{
		Mode:       packages.NeedName | packages.NeedTypes | packages.NeedImports | packages.NeedDeps,
		Dir:        cfg.Dir,
		BuildFlags: []string{"-tags", "norlpgen"},
	}
	// 返回一个go包
	ps, err := packages.Load(pcfg, pathOfPackageRLP, ".")
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 {
		return nil, fmt.Errorf("no Go package found in %s", cfg.Dir)
	}

	packages.PrintErrors(ps)

	// Find the packages that were loaded.
	// 查找已加载的包。
	var (
		pkg        *types.Package
		packageRLP *types.Package
	)
	for _, p := range ps {
		if len(p.Errors) > 0 {
			return nil, fmt.Errorf("package %s has errors", p.PkgPath)
		}
		if p.PkgPath == pathOfPackageRLP {
			packageRLP = p.Types
		} else {
			pkg = p.Types
		}
	}
	bctx := newBuildContext(packageRLP)

	// Find the type and generate.
	typ, err := lookupStructType(pkg.Scope(), cfg.Type)
	if err != nil {
		return nil, fmt.Errorf("can't find %s in %s: %v", typ, pkg, err)
	}
	code, err = bctx.generate(typ, cfg.GenerateEncoder, cfg.GenerateDecoder)
	if err != nil {
		return nil, err
	}

	// Add build comments.
	// This is done here to avoid processing these lines with gofmt.
	var header bytes.Buffer
	fmt.Fprint(&header, "// Code generated by rlpgen. DO NOT EDIT.\n\n")
	fmt.Fprint(&header, "//go:build !norlpgen\n")
	fmt.Fprint(&header, "// +build !norlpgen\n\n")
	return append(header.Bytes(), code...), nil
}

func lookupStructType(scope *types.Scope, name string) (*types.Named, error) {
	typ, err := lookupType(scope, name)
	if err != nil {
		return nil, err
	}
	_, ok := typ.Underlying().(*types.Struct)
	if !ok {
		return nil, errors.New("not a struct type")
	}
	return typ, nil
}

func lookupType(scope *types.Scope, name string) (*types.Named, error) {
	obj := scope.Lookup(name)
	if obj == nil {
		return nil, errors.New("no such identifier")
	}
	typ, ok := obj.(*types.TypeName)
	if !ok {
		return nil, errors.New("not a type")
	}
	return typ.Type().(*types.Named), nil
}