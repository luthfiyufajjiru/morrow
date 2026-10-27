// Command verifydist checks that every release artifact in a dist directory
// was built for the OS and architecture its file name claims. It guards
// against cross-compiles that silently produce host binaries.
//
// Usage: go run ./script/verifydist dist/v<version>
package main

import (
	"debug/elf"
	"debug/pe"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type target struct {
	suffix  string // file name suffix, after "morrow-v<version>_"
	elf     elf.Machine
	peMach  uint16
	isMSI   bool
	display string
}

var targets = []target{
	{suffix: "_windows-x64.exe", peMach: pe.IMAGE_FILE_MACHINE_AMD64, display: "PE x86-64"},
	{suffix: "_windows-x86.exe", peMach: pe.IMAGE_FILE_MACHINE_I386, display: "PE i386"},
	{suffix: "_windows-arm64.exe", peMach: pe.IMAGE_FILE_MACHINE_ARM64, display: "PE ARM64"},
	{suffix: "_windows-x64.msi", peMach: pe.IMAGE_FILE_MACHINE_AMD64, isMSI: true, display: "MSI with PE x86-64"},
	{suffix: "_windows-x86.msi", peMach: pe.IMAGE_FILE_MACHINE_I386, isMSI: true, display: "MSI with PE i386"},
	{suffix: "_windows-arm64.msi", peMach: pe.IMAGE_FILE_MACHINE_ARM64, isMSI: true, display: "MSI with PE ARM64"},
	{suffix: "_linux-amd64", elf: elf.EM_X86_64, display: "ELF x86-64"},
	{suffix: "_linux-arm64", elf: elf.EM_AARCH64, display: "ELF aarch64"},
	{suffix: "_linux-arm", elf: elf.EM_ARM, display: "ELF ARM"},
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: verifydist <dist dir>")
		os.Exit(2)
	}
	dir := os.Args[1]
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	failed, checked := false, 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		t, ok := match(e.Name())
		if !ok {
			continue
		}
		checked++
		path := filepath.Join(dir, e.Name())
		if err := check(path, t); err != nil {
			fmt.Printf("FAIL %s: %v\n", e.Name(), err)
			failed = true
			continue
		}
		fmt.Printf("ok   %s (%s)\n", e.Name(), t.display)
	}
	if checked == 0 {
		fmt.Fprintf(os.Stderr, "no artifacts found in %s\n", dir)
		os.Exit(1)
	}
	if failed {
		os.Exit(1)
	}
}

func match(name string) (target, bool) {
	if !strings.HasPrefix(name, "morrow-v") {
		return target{}, false
	}
	for _, t := range targets {
		if strings.HasSuffix(name, t.suffix) {
			return t, true
		}
	}
	return target{}, false
}

func check(path string, t target) error {
	switch {
	case t.isMSI:
		return checkMSI(path, t.peMach)
	case t.peMach != 0:
		return checkPE(path, t.peMach)
	default:
		return checkELF(path, t.elf)
	}
}

func checkELF(path string, want elf.Machine) error {
	f, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("not an ELF file: %w", err)
	}
	defer f.Close()
	if f.Machine != want {
		return fmt.Errorf("ELF machine %v, want %v", f.Machine, want)
	}
	return nil
}

func checkPE(path string, want uint16) error {
	f, err := pe.Open(path)
	if err != nil {
		return fmt.Errorf("not a PE file: %w", err)
	}
	defer f.Close()
	if f.Machine != want {
		return fmt.Errorf("PE machine %#04x, want %#04x", f.Machine, want)
	}
	return nil
}

// checkMSI unpacks the installer with an administrative install and checks
// the morrow.exe inside it.
func checkMSI(path string, want uint16) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("MSI checks need msiexec (Windows only)")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "verifydist-msi-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	out, err := exec.Command("msiexec", "/a", abs, "/qn", "TARGETDIR="+tmp).CombinedOutput()
	if err != nil {
		return fmt.Errorf("msiexec /a: %v %s", err, out)
	}
	var exes []string
	filepath.WalkDir(tmp, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(d.Name(), "morrow.exe") {
			exes = append(exes, p)
		}
		return nil
	})
	if len(exes) != 1 {
		return fmt.Errorf("found %d morrow.exe in the MSI, want 1", len(exes))
	}
	return checkPE(exes[0], want)
}
