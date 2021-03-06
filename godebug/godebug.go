/*
Package godebug implements helper functions for debugging Go programs.
*/
package godebug // import "mgk.ro/godebug"

import (
	"debug/elf"
	"debug/gosym"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"

	"mgk.ro/uprobes"
)

// ProgLoadAddr returns the program load address. It's useful to calculate
// file offset from VA for uprobes.
func ProgLoadAddr(f *elf.File) uint64 {
	for _, p := range f.Progs {
		if p.Type == elf.PT_LOAD && p.Flags == elf.PF_X|elf.PF_R {
			return p.Vaddr - p.Off
		}
	}
	panic("program load address not found")
}

// Prog is a representation of the debugged program.
type Prog struct {
	*elf.File
	*gosym.Table

	path string
	load uint64
}

func NewProg(cmd *exec.Cmd) (*Prog, error) {
	file := cmd.Path
	if !filepath.IsAbs(file) {
		file = cmd.Dir + cmd.Path
	}
	f, err := elf.Open(file)
	if err != nil {
		return nil, err
	}
	symdat, err := f.Section(".gosymtab").Data()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("reading %s gosymtab: %v", file, err)
	}
	pclndat, err := f.Section(".gopclntab").Data()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("reading %s gopclntab: %v", file, err)
	}

	pcln := gosym.NewLineTable(pclndat, f.Section(".text").Addr)
	tab, err := gosym.NewTable(symdat, pcln)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("parsing %s gosymtab: %v", file, err)
	}
	prg := &Prog{
		File: f,
		Table: tab,
		load: ProgLoadAddr(f),
		path: file,
	}
	return prg, nil
}

// FuncOffset returns the offset of the named function in the memory
// image. This offset is used by uprobes.
func (p *Prog) FuncOffset(name string) uint64 {
	fn := p.LookupFunc(name)
	if fn == nil {
		panic("can't find function " + name)
	}
	return FuncOffset(fn, p.load)
}

// FuncOffset returns the offset of the function in the memory
// image. This offset is used by uprobes.
func FuncOffset(fn *gosym.Func, load uint64) uint64 {
	return fn.Entry - load
}

// Uprobe will return an uprobes event suitable for tracing the specified
// function.
func Uprobe(p *Prog, fn *gosym.Func) *uprobes.Event {
	ev := uprobes.NewEvent(Uglify(fn.Name), p.path, FuncOffset(fn, p.load)).Stack("h0", 1).U64().Stack("d0", 1).S64().Stack("h1", 2).U64().Stack("d1", 2).S64().Stack("h2", 3).U64().Stack("d2", 3).S64().Stack("h3", 4).U64().Stack("d3", 4).S64()
	return ev
}

// UretProbe will return an uretprobe event suitable for tracing the
// specified function return.
func UretProbe(p *Prog, fn *gosym.Func) *uprobes.Event {
	ev := uprobes.NewEvent(Uglify(fn.Name)+"_ret", p.path, FuncOffset(fn, p.load)).Return()
	return ev
}

var ugly = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// Uglify takes a nice Go name like net/http.(*response).WriteHeader and
// turns it into net__http______response____WriteHeader. This is because
// uprobes can only accept these retarded names.
func Uglify(name string) string {
	return ugly.ReplaceAllLiteralString(name, "__")
}
