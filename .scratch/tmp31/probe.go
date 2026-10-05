package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"os"
)

func main() {
	path := os.Args[1]
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	fmt.Println("file size:", len(data))
	peOff := int(binary.LittleEndian.Uint32(data[0x3c:]))
	fmt.Printf("e_lfanew=%d sig=%q\n", peOff, data[peOff:peOff+4])
	machine := binary.LittleEndian.Uint16(data[peOff+4:])
	numSec := binary.LittleEndian.Uint16(data[peOff+6:])
	optSize := binary.LittleEndian.Uint16(data[peOff+20:])
	fmt.Printf("machine=%#x numSections=%d optSize=%d\n", machine, numSec, optSize)
	opt := peOff + 24
	magic := binary.LittleEndian.Uint16(data[opt:])
	fmt.Printf("optMagic=%#x\n", magic)
	var sa, fa, sizeOfImage, sizeOfHeaders, checksum uint32
	var dataDirOff int
	var numRva int
	switch magic {
	case 0x20b:
		sa = binary.LittleEndian.Uint32(data[opt+32:])
		fa = binary.LittleEndian.Uint32(data[opt+36:])
		sizeOfImage = binary.LittleEndian.Uint32(data[opt+56:])
		sizeOfHeaders = binary.LittleEndian.Uint32(data[opt+60:])
		checksum = binary.LittleEndian.Uint32(data[opt+64:])
		numRva = int(binary.LittleEndian.Uint32(data[opt+108:]))
		dataDirOff = opt + 112
	case 0x10b:
		sa = binary.LittleEndian.Uint32(data[opt+28:])
		fa = binary.LittleEndian.Uint32(data[opt+32:])
		sizeOfImage = binary.LittleEndian.Uint32(data[opt+52:])
		sizeOfHeaders = binary.LittleEndian.Uint32(data[opt+56:])
		checksum = binary.LittleEndian.Uint32(data[opt+60:])
		numRva = int(binary.LittleEndian.Uint32(data[opt+88:]))
		dataDirOff = opt + 92
	}
	fmt.Printf("sa=%d fa=%d sizeOfImage=%#x sizeOfHeaders=%#x checksum=%#x numRva=%d\n", sa, fa, sizeOfImage, sizeOfHeaders, checksum, numRva)
	for i := 0; i < numRva && i < 16; i++ {
		rva := binary.LittleEndian.Uint32(data[dataDirOff+i*8:])
		size := binary.LittleEndian.Uint32(data[dataDirOff+i*8+4:])
		if rva != 0 || size != 0 {
			fmt.Printf("datadir[%d] rva=%#x size=%d\n", i, rva, size)
		}
	}
	st := opt + int(optSize)
	for i := 0; i < int(numSec); i++ {
		h := data[st+i*40 : st+i*40+40]
		name := string(bytes.TrimRight(h[:8], "\x00"))
		vsize := binary.LittleEndian.Uint32(h[8:])
		va := binary.LittleEndian.Uint32(h[12:])
		rawsz := binary.LittleEndian.Uint32(h[16:])
		rawptr := binary.LittleEndian.Uint32(h[20:])
		fmt.Printf("sec %-10s vsize=%-8d va=%-10#x rawsz=%-8d rawptr=%-10#x rawEnd=%d\n", name, vsize, va, rawsz, rawptr, rawptr+rawsz)
	}
	symPtr := binary.LittleEndian.Uint32(data[peOff+8:])
	numSym := binary.LittleEndian.Uint32(data[peOff+12:])
	fmt.Printf("symtab ptr=%#x n=%d\n", symPtr, numSym)
	f, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		fmt.Println("pe.NewFile err:", err)
		return
	}
	defer f.Close()
	libs, err := f.ImportedLibraries()
	fmt.Println("ImportedLibraries:", libs, "err:", err)
	syms, err := f.ImportedSymbols()
	if len(syms) > 8 {
		syms = syms[:8]
	}
	fmt.Println("ImportedSymbols(8):", syms, "err:", err)
	fmt.Printf("Machine=%#x sections=%d\n", f.Machine, len(f.Sections))
}
