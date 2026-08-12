// Release-binary verification gate for the catalog seed SHA.
//
// The release pipeline (release.yml) builds a binary via
// `go build -ldflags "-X ...FirstRunCommitSHA=<sha>"` and runs
// scripts/verify-release.sh against it before uploading. This
// file gives the same gate a Go-callable surface so unit tests
// can exercise it without spawning a shell.
//
// Spec background (openspec/changes/slice-2/specs/release-sha-bake/spec.md):
// a release MUST be blocked if the runtime value of
// FirstRunCommitSHA equals the placeholder literal
// `<PLACEHOLDER_SHA>` (the value it ships with from
// pinning.go) or does not equal the SHA the release pipeline
// baked. VerifyBakedSHA is the Go-callable form of that gate:
// it parses the runtime symbol from the binary via
// debug/{macho,elf,pe}, dereferences the string data pointer,
// and compares the resulting bytes against the expected SHA.
//
// Why this implementation is not a `strings | grep`-style test:
//
// The previous release gate (VerifyNoPlaceholder, PR-E #4.3)
// scanned the binary's bytes for the literal `<PLACEHOLDER_SHA>`
// and rejected any binary that contained it. That gate kept
// flagging correctly baked binaries because the Go linker
// preserves the source-code literal in rodata ADJACENT to the
// runtime value of the rewritten string variable; the substring
// search reads the literal bytes from rodata even though the
// runtime value at FirstRunCommitSHA points to a freshly
// allocated rodata block holding the baked SHA. The grep gate
// was unwound in favour of the symbol-table read implemented by
// VerifyBakedSHA, which sees the runtime value rather than the
// linker-preserved literal.
package catalog

import (
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"errors"
	"fmt"
	"os"
)

// PlaceholderLiteral is the exact substring the release gate
// uses to classify a "literal still in rodata" run as a failed
// release. It is exported so the bash script, the new
// `verify-release-binary` CLI subcommand, and any downstream
// tooling share the same constant.
const PlaceholderLiteral = "<PLACEHOLDER_SHA>"

// FirstRunCommitSHASymbol is the exact path the Go linker writes
// into the binary's symbol table for the runtime variable
// declared in pinning.go. Tests assert against this constant
// rather than reconstructing the path inline so a future rename
// of the variable surfaces in one place, not every test body.
const FirstRunCommitSHASymbol = "github.com/asolis87/lo-fi-player/internal/catalog.FirstRunCommitSHA"

// ErrPlaceholderFound is wrapped around every VerifyBakedSHA
// failure whose root cause is the source-code placeholder
// literal being the RUNTIME value of FirstRunCommitSHA (the
// binary was never baked). Callers match with errors.Is so a CI
// step can branch on the failure class.
var ErrPlaceholderFound = errors.New("catalog: FirstRunCommitSHA still holds <PLACEHOLDER_SHA>")

// ErrSHAMismatch is wrapped around every VerifyBakedSHA failure
// whose root cause is the runtime value of FirstRunCommitSHA
// being a SHA different from expectedSHA (the binary was
// baked but to the wrong SHA). Distinct from ErrPlaceholderFound
// so the operator can tell "the pipeline never rewrote the
// variable" from "the pipeline rewrote the variable but with a
// stale or wrong SHA".
var ErrSHAMismatch = errors.New("catalog: FirstRunCommitSHA does not match expected")

// ErrBinaryUnreadable is wrapped around every VerifyBakedSHA
// failure whose root cause is the file itself (missing,
// permission-denied, not a regular file, or in an unsupported
// binary format). Callers match with errors.Is so a missing
// artifact can be distinguished from a placeholder leak or a
// SHA mismatch.
var ErrBinaryUnreadable = errors.New("catalog: cannot read release binary")

// VerifyBakedSHA returns nil iff the binary at binaryPath has
// FirstRunCommitSHA baked to expectedSHA at link time. The
// implementation walks the runtime symbol table, dereferences
// the string data pointer, and compares the resulting bytes
// against expectedSHA. Three failure classes are returned:
//
//   - ErrPlaceholderFound: the runtime value equals the
//     source-code placeholder literal (binary was never baked).
//   - ErrSHAMismatch: the runtime value is a different SHA
//     (binary was baked, but to the wrong SHA).
//   - ErrBinaryUnreadable: the file is missing, unreadable, or
//     in an unsupported binary format.
//
// The function is the Go-callable counterpart to
// scripts/verify-release.sh: both call paths funnel here so the
// bash script and the unit tests share one implementation.
func VerifyBakedSHA(binaryPath, expectedSHA string) error {
	data, err := goStringSymbolBytes(binaryPath, FirstRunCommitSHASymbol)
	if err != nil {
		if errors.Is(err, ErrBinaryUnreadable) {
			return err
		}
		return fmt.Errorf("%w: %s: %v", ErrBinaryUnreadable, binaryPath, err)
	}
	switch {
	case string(data) == expectedSHA:
		return nil
	case containsData(data, PlaceholderLiteral):
		return fmt.Errorf("%w: path=%s, runtime=%q", ErrPlaceholderFound, binaryPath, data)
	default:
		return fmt.Errorf("%w: path=%s, runtime=%q want %q", ErrSHAMismatch, binaryPath, data, expectedSHA)
	}
}

// goStringSymbolBytes returns the runtime bytes of the string
// variable named symName in the binary at binaryPath. For a Go
// `var x = "..."` declaration, the linker places a 16-byte
// struct (`{Data uintptr; Len int}`) at the variable's address;
// the struct's `Data` points into rodata, where the actual
// string bytes live. This helper reads both layers and returns
// the rodata bytes.
//
// Three binary formats are supported:
//
//   - Mach-O: macOS, GOOS=darwin. Parsed first because the
//     default `lofi` binary on a developer workstation is a
//     Mach-O.
//   - ELF: Linux, GOOS=linux. Parsed second because CI runs on
//     Ubuntu.
//   - PE: Windows, GOOS=windows. Parsed when the path ends in
//     `.exe`, before the bare-name fallback tries Mach-O.
//
// The function returns ErrBinaryUnreadable when it cannot
// recognise the format or the symbol is missing. The Mach-O and
// ELF branches read their file headers via debug/{macho,elf},
// which works for the local CPU architecture; cross-arch
// binaries (e.g. an amd64 lofi inspected on an arm64 dev box)
// require a separate fat-binary reader and are out of scope
// until the project ships a fat-binary release artifact.
func goStringSymbolBytes(binaryPath, symName string) ([]byte, error) {
	f, err := os.Open(binaryPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrBinaryUnreadable, binaryPath, err)
	}
	defer f.Close()

	// PE (.exe) is the only format keyed on filename; the bare
	// path tries Mach-O first (macOS), then ELF (Linux). The
	// two readers are mutually exclusive here so the CLI can
	// ingest `.exe` from a Windows release without bumping into
	// the PE magic.
	switch {
	case peMagic(binaryPath):
		data, err := peStringAtSymbol(f, symName)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrBinaryUnreadable, binaryPath, err)
		}
		return data, nil
	case machoMagic(binaryPath):
		data, err := machoStringAtSymbol(f, symName)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrBinaryUnreadable, binaryPath, err)
		}
		return data, nil
	case elfMagic(binaryPath):
		data, err := elfStringAtSymbol(f, symName)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrBinaryUnreadable, binaryPath, err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("%w: %s: unsupported binary format", ErrBinaryUnreadable, binaryPath)
	}
}

// peMagic reports whether the binary path is a Windows PE
// (.exe). Used to dispatch from goStringSymbolBytes; the PE
// reader itself is strict on the magic and will reject anything
// that does not start with "MZ", so this is a routing hint
// rather than a full format check.
func peMagic(path string) bool {
	for _, ext := range []string{".exe", ".EXE"} {
		if len(path) >= len(ext) && path[len(path)-len(ext):] == ext {
			return true
		}
	}
	return false
}

// machoMagic reports whether the binary looks like Mach-O by
// sniffing the magic. The actual format check lives inside
// macho.NewFile; this peeks at the first 4 bytes so the
// dispatcher can confidently pick Mach-O over ELF on macOS.
func machoMagic(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var hdr [4]byte
	n, _ := f.Read(hdr[:])
	if n < 4 {
		return false
	}
	// Mach-O 32-bit: 0xFEEDFACE; 64-bit: 0xFEEDFACF (little-endian
	// first byte is 0xCF for 64-bit). Both are unambiguous.
	return hdr[0] == 0xCF || hdr == [4]byte{0xFE, 0xED, 0xFA, 0xCE} || hdr == [4]byte{0xCE, 0xFA, 0xED, 0xFE}
}

// elfMagic reports whether the binary looks like ELF by
// sniffing the magic. ELF starts with the literal bytes
// 0x7F 'E' 'L' 'F'.
func elfMagic(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var hdr [4]byte
	n, _ := f.Read(hdr[:])
	if n < 4 {
		return false
	}
	return hdr[0] == 0x7F && hdr[1] == 'E' && hdr[2] == 'L' && hdr[3] == 'F'
}

// machoStringAtSymbol returns the runtime string bytes of the
// variable symName in a Mach-O binary. Reads the symbol table
// for symName (type N_SECT, sect > 0), then reads the 16-byte
// struct at the symbol's section offset to recover the
// `{Data uintptr; Len int}` of the runtime string and follows
// the data pointer to rodata.
func machoStringAtSymbol(f *os.File, symName string) ([]byte, error) {
	mf, err := macho.NewFile(f)
	if err != nil {
		return nil, err
	}
	defer mf.Close()
	if mf.Symtab == nil {
		return nil, fmt.Errorf("binary has no symbol table")
	}
	for _, sym := range mf.Symtab.Syms {
		if sym.Name != symName {
			continue
		}
		if !isMachOSectDataSym(sym.Type, sym.Sect) {
			continue
		}
		return readMachoString(mf, sym)
	}
	return nil, fmt.Errorf("symbol %q not found", symName)
}

// isMachOSectDataSym reports whether a Mach-O symbol is a
// section-relative data symbol. Mach-O's N_TYPE field is the
// low 3 bits of the Type byte; N_SECT = 0x0e. A real
// section-relative symbol must also have a non-zero Sect.
func isMachOSectDataSym(t, sect uint8) bool {
	const nType = 0x0e
	const stab = 0xe0
	if t&stab != 0 {
		return false
	}
	if t&nType != nType {
		return false
	}
	return sect > 0
}

// readMachoString reads the 16-byte string struct that lives at
// sym's section address, then follows the data pointer into
// rodata to fetch strLen bytes. The 16-byte layout is
// `{Data uintptr; Len int}` on a 64-bit binary; on 32-bit it is
// 8 bytes total. We read 16 bytes either way so the function is
// safe on the CI's amd64 Linux binaries and the developer's
// arm64 macOS binary.
func readMachoString(mf *macho.File, sym macho.Symbol) ([]byte, error) {
	sect := mf.Sections[sym.Sect-1]
	off := uint64(sym.Value - sect.Addr)
	const stringStructSize = 16
	var buf [stringStructSize]byte
	if _, err := sect.ReadAt(buf[:], int64(off)); err != nil {
		return nil, fmt.Errorf("read symbol table: %v", err)
	}
	dataPtr := readUint64(buf[0:8])
	strLen := readUint64(buf[8:16])
	if strLen == 0 {
		return nil, fmt.Errorf("symbol %q has zero-length runtime string", sym.Name)
	}
	return readBytesAtVM(mf.Sections, dataPtr, strLen)
}

// readUint64 reads a little-endian uint64 from b. Centralised
// so the byte order (Mach-O and ELF on x86/aarch64 are
// little-endian; a future big-endian port would touch this
// helper rather than every caller) is easy to flip in one place.
func readUint64(b []byte) uint64 {
	var v uint64
	for i, by := range b {
		v |= uint64(by) << (8 * uint(i))
	}
	return v
}

// readBytesAtVM reads n bytes from the file at the VM address
// addr, looking up which section contains addr. Returns an
// error if no section covers the address.
func readBytesAtVM(sections []*macho.Section, addr uint64, n uint64) ([]byte, error) {
	for _, s := range sections {
		if addr < s.Addr || addr >= s.Addr+s.Size {
			continue
		}
		buf := make([]byte, n)
		if _, err := s.ReadAt(buf, int64(addr-s.Addr)); err != nil {
			return nil, fmt.Errorf("read rodata at %#x: %v", addr, err)
		}
		return buf, nil
	}
	return nil, fmt.Errorf("VM address %#x is not in any known section", addr)
}

// elfStringAtSymbol mirrors machoStringAtSymbol but for ELF.
// ELF symbol tables differentiate types via the Info byte's low
// nibble (STT_*); data symbols are STT_NOTYPE (0) when Go
// outputs them, so we accept any non-zero STB_GLOBAL/STB_LOCAL
// with a non-shared binding. The simplest, most permissive
// filter in practice: any symbol whose Name matches and whose
// SectionIndex points to a real section with the SHF_ALLOC
// flag is the variable we want; we cannot rely on Info
// because Go's ELF output uses a non-standard type.
func elfStringAtSymbol(f *os.File, symName string) ([]byte, error) {
	ef, err := elf.NewFile(f)
	if err != nil {
		return nil, err
	}
	defer ef.Close()
	syms, err := ef.Symbols()
	if err != nil {
		return nil, fmt.Errorf("read ELF symbols: %v", err)
	}
	for _, sym := range syms {
		if sym.Name != symName {
			continue
		}
		if sym.Section == elf.SHN_UNDEF {
			continue
		}
		return readElfString(ef, sym)
	}
	return nil, fmt.Errorf("symbol %q not found", symName)
}

// readElfString is the ELF counterpart of readMachoString: read
// the 16-byte struct at the symbol's section address, follow
// the data pointer into a different section holding rodata.
func readElfString(ef *elf.File, sym elf.Symbol) ([]byte, error) {
	sect := ef.Sections[sym.Section]
	if sect == nil {
		return nil, fmt.Errorf("symbol %q has no section", sym.Name)
	}
	const stringStructSize = 16
	var buf [stringStructSize]byte
	// sym.Value is the symbol's virtual address, not its file
	// offset. Subtract the section's load address to get the
	// offset within the section's bytes; without this, on Linux
	// the offset is a multi-megabyte virtual address and the
	// read returns EOF before reading the 16-byte string struct.
	if _, err := sect.ReadAt(buf[:], int64(sym.Value-sect.Addr)); err != nil {
		return nil, fmt.Errorf("read symbol table: %v", err)
	}
	dataPtr := readUint64(buf[0:8])
	strLen := readUint64(buf[8:16])
	if strLen == 0 {
		return nil, fmt.Errorf("symbol %q has zero-length runtime string", sym.Name)
	}
	for _, s := range ef.Sections {
		if dataPtr < s.Addr || dataPtr >= s.Addr+s.Size {
			continue
		}
		out := make([]byte, strLen)
		if _, err := s.ReadAt(out, int64(dataPtr-s.Addr)); err != nil {
			return nil, fmt.Errorf("read rodata at %#x: %v", dataPtr, err)
		}
		return out, nil
	}
	return nil, fmt.Errorf("data pointer %#x not in any ELF section", dataPtr)
}

// peStringAtSymbol is the PE counterpart. Go's PE output places
// runtime variables in the .data section (and reads via the
// same 16-byte `{Data,Len}` layout). The PE file header is read
// with debug/pe; symbol lookup uses the COFF symbol table if
// present, or the deferred-import table otherwise. Because
// `-X` bakes a string into the rodata section, we accept any
// symbol whose name matches regardless of SectionNumber, then
// walk the 16-byte struct the same way.
func peStringAtSymbol(f *os.File, symName string) ([]byte, error) {
	pf, err := pe.NewFile(f)
	if err != nil {
		return nil, err
	}
	defer pf.Close()
	for _, sym := range pf.Symbols {
		if sym.Name != symName {
			continue
		}
		sect := pf.Sections[sym.SectionNumber-1]
		if sect == nil {
			continue
		}
		const stringStructSize = 16
		var buf [stringStructSize]byte
		// sym.Value is the symbol's virtual address; subtract
		// the section's load address to get the file offset.
		if _, err := sect.ReadAt(buf[:], int64(uint64(sym.Value)-uint64(sect.VirtualAddress))); err != nil {
			return nil, fmt.Errorf("read PE symbol: %v", err)
		}
		dataPtr := readUint64(buf[0:8])
		strLen := readUint64(buf[8:16])
		if strLen == 0 {
			return nil, fmt.Errorf("symbol %q has zero-length runtime string", sym.Name)
		}
		out := make([]byte, strLen)
		if _, err := sect.ReadAt(out, int64(dataPtr-uint64(sect.VirtualAddress))); err != nil {
			return nil, fmt.Errorf("read PE rodata at %#x: %v", dataPtr, err)
		}
		return out, nil
	}
	return nil, fmt.Errorf("symbol %q not found", symName)
}

// containsData is bytes.Contains renamed to a private helper so
// release.go does not pull the bytes package into the production
// code path's import surface.
func containsData(haystack []byte, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}
