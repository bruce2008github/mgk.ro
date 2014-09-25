/*
7lfix: refactor arm64 Plan 9 linker
	7lfix [files ...]

7lfix helps refactor Plan 9 linkers into liblink form used by Go.
*/
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"

	"code.google.com/p/rsc/cc"

	_ "code.google.com/p/rbits/log"
)

const (
	theChar = 7
	ld = "7l"
	lddir = "/src/cmd/" + ld
)

// iomap maps each input file to its corresponding output file.
// Unknown files go to zzz.c.
// missing *.h pstate.c main.c.
var iomap = map[string]string{
	"dyn.c":    "asm7.c",
	"sub.c":    "xxx.c",
	"mod.c":    "xxx.c",
	"list.c":   "list7.c",
	"noop.c":   "obj7.c",
	"elf.c":    "xxx.c",
	"pass.c":   "obj7.c",
	"pobj.c":   "xxx.c",
	"asm.c":    "asm7.c",
	"optab.c":  "asm7.c",
	"obj.c":    "obj7.c",
	"span.c":   "asm7.c",
	"asmout.c": "asm7.c",
}

// symbols to start from
var start = []string{
	"span",
	"asmout",
	"chipfloat",
	"follow",
	"noops",
	"listinit",
}

var includes = `#include <u.h>
#include <libc.h>
#include <bio.h>
#include <link.h>
#include "../cmd/7l/7.out.h"
`

// symbols is a symbol table.
type symbols map[*cc.Decl][]*cc.Decl

// deps is the dependency graph between symbols.
var deps = symbols{}

// all are all the symbols, for quick access
var all = symbols{}

// replace unqualified names in iomap with full paths.
func init() {
	for k, v := range iomap {
		iomap[runtime.GOROOT() + lddir + "/" + k] = v
		delete(iomap, k)
	}
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: 7lfix\n")
		fmt.Fprintf(os.Stderr, "\tfiles are from $GOOROT" + lddir)
		os.Exit(1)
	}
	if flag.NArg() != 0 {
		flag.Usage()
	}
	prog := parse()
	symtab(prog)
	dep(prog)
	var syms []*cc.Decl
	for _, v := range start {
		syms = append(syms, deps.lookup(v))
	}
	subset := extract(syms...)
	print(subset, "liblink")
}

// parse opens and parses all input files, and returns the result as
// a *cc.Prog.
func parse() *cc.Prog {
	var r []io.Reader
	var files []string
	for name, _ := range iomap {
		f, err := os.Open(name)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		r = append(r, f)
		files = append(files, name)
	}
	prog, err := cc.ReadMany(files, r)
	if err != nil {
		log.Fatal(err)
	}
	return prog
}

// print pretty prints fns (for which x.Type.Is(cc.Func) must be true)
// into dir.
func print(fns []*cc.Decl, dir string) {
	err := os.RemoveAll(dir)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0775); err != nil {
		log.Fatal(err)
	}
	file := make(map[string]*os.File)
	for _, v := range fns {
		if !strings.Contains(v.Span.String(), ld) {
			continue
		}
		name, ok := iomap[v.Span.Start.File]
		if !ok {
			if strings.Contains(v.Span.Start.File, ".h") {
				name = "l.h"
			} else {
				name = "zzz.c"
			}
		}
		f, ok := file[name]
		if !ok {
			// fmt.Printf("%v:	%v\n", dir + "/" + name, file)
			f, err = os.Create(dir + "/" + name)
			if err != nil {
				log.Fatal(err)
			}
			defer f.Close()
			file[name] = f
			f.WriteString("//+build ignore\n\n")
			if strings.Contains(v.Span.Start.File, ".c") {
				f.WriteString(includes)
				f.WriteString("\n")
			}
		}
		var pp cc.Printer
		pp.Print(v)
		f.Write(pp.Bytes())
		f.WriteString("\n\n")
	}
}

func dep(prog *cc.Prog) {
	var curfunc *cc.Decl
	cc.Preorder(prog, func(x cc.Syntax) {
		switch x := x.(type) {
		case *cc.Decl:
			// A function declaration. Function prototypes in the
			// middle of a function would probably break our plans,
			// but we hope for the best.
			if x.Type.Is(cc.Func) {
				deps[x] = nil
				curfunc = x
			}
		case *cc.Expr:
			switch x.Op {
			// Using a name for a function address.
			case cc.Name:
				if curfunc.Name == "listinit" {
					fmt.Println("in", curfunc.Name, curfunc.GetSpan())
					fmt.Println("	looking for", x.Text, x.GetSpan())
				}
				xfn := all.lookup(x.Text)
				if xfn == nil {
					return
				}
				for _, v := range deps[curfunc] {
					if xfn == v {
						return
					}
				}
				deps[curfunc] = append(deps[curfunc], xfn)
			// Take a function's address.
			case cc.Addr:
				if curfunc.Name == "listinit" {
					fmt.Printf("in listinit, x=%#v\n", x)
				}
				if x.Left == nil || x.Left.XDecl == nil {
					return
				}
				if !x.Left.XDecl.Type.Is(cc.Func) {
					return
				}
				for _, v := range deps[curfunc] {
					if x.Left.XDecl == v {
						return
					}
				}
				deps[curfunc] = append(deps[curfunc], x.Left.XDecl)
			// Direct function call.
			case cc.Call:
				for _, v := range deps[curfunc] {
					if x.Left.XDecl == v {
						return
					}
				}
				deps[curfunc] = append(deps[curfunc], x.Left.XDecl)
			}
		}
	})
}

// symtab populates the all symbol table.
func symtab(prog *cc.Prog) {
	cc.Preorder(prog, func(x cc.Syntax) {
		d, ok := x.(*cc.Decl)
		if !ok {
			return
		}
		if !d.Type.Is(cc.Func) {
			return
		}
		all[d] = nil
	})
}

func (st symbols) lookup(name string) *cc.Decl {
	for s := range st {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// extract returns the recursive list of functions called by the fns
// x.Type.Is(cc.Func) must be true for fns, and will be true for subset.
func extract(fns ...*cc.Decl) (subset []*cc.Decl) {
	var r func(f *cc.Decl)
	r = func(f *cc.Decl) {
		for _, s := range subset {
			if s == f {
				return
			}
		}
		subset = append(subset, f)
		for _, d := range deps[f] {
			r(d)
		}
	}
	for _, v := range fns {
		r(v)
	}
	return
}