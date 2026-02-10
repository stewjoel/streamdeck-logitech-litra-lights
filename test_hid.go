package main; /*
#cgo LDFLAGS: -L. -lhidapi -static-libgcc
#include "hidapi/include/hidapi.h"
*/
import "C"; import "fmt"; func main() { res := C.hid_init(); fmt.Println("Result:", res) }
