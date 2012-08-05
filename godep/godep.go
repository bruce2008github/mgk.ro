// Permission to use, copy, modify, and/or distribute this software for
// any purpose is hereby granted, provided this notice appear in all copies.

/*
Godep prints dependency information for packages named by the import
paths.

Usage:
	godep [options] [packages]

By default it prints a dependency graph that spans all packages.

The options are:
	-p
		print individial imports for each named package
	-tags
		additional build tags to consider satisfied

For more about specifying packages, see 'go help packages'.
*/
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/build"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	_ "code.google.com/p/rbits/log"
)

var (
	flagP    = flag.Bool("p", false, "print individial imports for each package")
	flagDot  = flag.Bool("dot", false, "print DOT language (GraphWiz)")
	flagPng  = flag.String("png", "", "write graph to png file")
	flagTags = flag.String("tags", "", "additional build tags to consider")
)

var (
	bldCtxt = build.Default
	pkgdep  = map[string][]string{} // pkg -> pkg dependencies.
	pkgs    []string                // user supplied.
)

type pkgStatus struct {
	visited bool
	printed bool
}

func (st pkgStatus) SetVisited() pkgStatus { st.visited = true; return st }
func (st pkgStatus) SetPrinted() pkgStatus { st.printed = true; return st }

var usageString = `usage: godep [options] [packages]
Options:
`

func usage() {
	fmt.Fprint(os.Stderr, usageString)
	flag.PrintDefaults()
	os.Exit(1)
}

func main() {
	flag.Usage = usage
	flag.Parse()

	bldCtxt.BuildTags = strings.Split(*flagTags, " ")
	golist(flag.Args()...) // finds packages to work with.
	for _, v := range pkgs {
		dfs(v)
	}
	visitedPkgs := make(map[string]pkgStatus)
	for _, v := range pkgs {
		switch {
		case *flagDot:
		case *flagPng != "":
			log.Fatal("-png flag not implemented")
		case *flagP:
			// redeclared because it's not shared between iterations.
			visitedPkgs := make(map[string]pkgStatus)
			fmt.Printf("%s ", v)
			printPkgDeps(v, visitedPkgs)
			fmt.Printf("\n")
		default:
			printDepTree(v, visitedPkgs)
		}
	}
}

// golist runs 'go list args' and assigns the result to pkgs.
func golist(args ...string) {
	args = append([]string{"list"}, args...)
	cmd := exec.Command("go", args...)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	r := bufio.NewReader(stdout)

	if err = cmd.Start(); err != nil {
		log.Fatal(err)
	}
	for {
		pkg, _, err := r.ReadLine()
		if err != nil {
			break
		}
		pkgs = append(pkgs, string(pkg))
	}
	if err = cmd.Wait(); err != nil {
		os.Exit(1)
	}
	return
}

// dfs does a depth-first traversal of the package dependency graph.
// path is the current node. It records the dependency information to
// pkgdep.
func dfs(path string) {
	pkg, err := bldCtxt.ImportDir(srcDir(path), 0)
	if err != nil {
		log.Fatal(err)
	}
	deps := pkg.Imports
	pkgdep[path] = deps
	for _, v := range deps {
		// C is a pseudopackage.
		if v == "C" {
			continue
		}
		_, ok := pkgdep[v]
		if !ok {
			dfs(v)
		}
	}
}

// printPkgDeps prints on a single line all packages imported by the
// named package.
func printPkgDeps(path string, visitedPkgs map[string]pkgStatus) {
	pkgStat, done := visitedPkgs[path]
	if done && pkgStat.visited {
		return
	}
	visitedPkgs[path] = pkgStat.SetVisited()

	deps := pkgdep[path]
	for _, v := range deps {
		if pkgStat := visitedPkgs[v]; pkgStat.printed == false {
			fmt.Printf("%s ", v)
			visitedPkgs[v] = pkgStat.SetPrinted()
		}
	}
	for _, v := range deps {
		printPkgDeps(v, visitedPkgs)
	}
}

// printDepTree prints the dependency tree one level per line.
func printDepTree(path string, visitedPkgs map[string]pkgStatus) {
	pkgStat, done := visitedPkgs[path]
	if done && pkgStat.visited {
		return
	}
	visitedPkgs[path] = pkgStat.SetVisited()

	deps := pkgdep[path]
	fmt.Printf("%s ", path)
	for _, v := range deps {
		fmt.Printf("%s ", v)
	}
	fmt.Printf("\n")
	for _, v := range deps {
		printDepTree(v, visitedPkgs)
	}
}

// srcDir returns the directory where the package with the named
// import path resides. It is required for resolving local imports (ugh).
func srcDir(path string) string {
	// Check if it's a command in $GOROOT/src, like cmd/go.
	cmdpath := filepath.Join(bldCtxt.GOROOT, "src", path)
	// normally we'd use build.ImportDir, but it has a bug.
	fi, err := os.Stat(cmdpath)
	if err != nil || !fi.IsDir() {
		// A regular package in $GOROOT/src/pkg or in any $GOPATH/src.
		pkg, err := bldCtxt.Import(path, "", build.FindOnly)
		if err != nil {
			log.Fatal(err)
		}
		return pkg.Dir
	}
	return cmdpath
}
