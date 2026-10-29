// C only, without C++, like a Go program linking a prebuilt Rust library such as wasmtime-go's:
// nothing pulls in an unwinder, the builder has to.
package main

/*
int has_register_frame(void);
*/
import "C"

import "fmt"

var version = "dev"

func main() {
	fmt.Printf("unwind %s register_frame=%v\n", version, C.has_register_frame() == 1)
}
