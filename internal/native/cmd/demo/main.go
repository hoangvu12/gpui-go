//go:build windows

// Command demo is the ticket02 observable consumer: an offline, pure-Go
// (CGO_ENABLED=0) program that loads the embedded native artifact, prints its
// verified identity and provenance, performs checked buffer round trips and
// the panic-containment probe, and releases the library exactly once.
package main

import (
	"fmt"
	"os"
	"strings"

	"gpui-go/internal/native"
)

func main() {
	fmt.Println("gpui-go native bootstrap demo (CGO_ENABLED=0, pure Go + syscall)")

	lib, err := native.Load(native.Options{})
	if err != nil {
		fmt.Println("FAIL: load:", err)
		os.Exit(1)
	}

	id := lib.Identity()
	fmt.Println()
	fmt.Println("identity (verified before any call):")
	pf := func(label string, value any) { fmt.Printf("  %-18s %v\n", label, value) }
	pf("name", id.Name)
	pf("sha256", id.SHA256)
	pf("bytes", id.Bytes)
	pf("gzip-9 bytes", id.GzipBytes)
	pf("ce commit", id.CECommit)
	pf("abi version", id.ABIVersion)
	pf("native revision", id.NativeRevision)
	pf("capabilities", fmt.Sprintf("%#x %s", id.Capabilities, id.CapabilityNames))
	pf("machine", id.Machine)
	pf("target", id.Target)
	pf("source", id.Source)
	pf("path", id.Path)
	pf("generation", id.Generation)
	pf("built at", id.BuiltAt)
	pf("rustc", id.Rustc)
	pf("crt", id.CRTStatic)
	pf("imports", strings.Join(id.Imports, ", "))

	fmt.Println()
	fmt.Println("buffer round trip:")
	pass := true
	for _, size := range []int{0, 1, 17, 4096} {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i % 251)
		}
		echo, checksum, err := lib.RoundTrip(data)
		if err != nil {
			fmt.Printf("  len %-5d FAIL: %v\n", size, err)
			pass = false
			continue
		}
		if string(echo) != string(data) {
			fmt.Printf("  len %-5d FAIL: echoed bytes differ\n", size)
			pass = false
			continue
		}
		fmt.Printf("  len %-5d echo ok, FNV-1a64 checksum %016x  PASS\n", size, checksum)
	}

	fmt.Println()
	if err := lib.PanicProbe(); err != nil {
		fmt.Println("panic containment: FAIL:", err)
		pass = false
	} else {
		fmt.Println("panic containment:  contained, status 7 reported        PASS")
	}

	if err := lib.Close(); err != nil {
		fmt.Println("release:            FAIL:", err)
		pass = false
	} else {
		fmt.Println("release:            exactly once (module stays resident) PASS")
	}

	fmt.Println()
	if pass {
		fmt.Println("demo: PASS")
	} else {
		fmt.Println("demo: FAIL")
		os.Exit(1)
	}
}
