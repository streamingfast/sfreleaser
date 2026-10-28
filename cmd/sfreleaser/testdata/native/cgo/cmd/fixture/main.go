package main

/*
#include <stdlib.h>
int add(int a, int b);
int vector_sum(int n);
*/
import "C"

import (
	"crypto/x509"
	"fmt"
	"net"
	"os/user"
)

var version = "dev"

func main() {
	_, certErr := x509.SystemCertPool()
	_, dnsErr := net.LookupHost("localhost")
	_, userErr := user.Current()
	fmt.Printf("fixture %s add=%d vector=%d certs=%v dns=%v user=%v platform=%v\n", version, C.add(2, 3), C.vector_sum(4), certErr == nil, dnsErr == nil, userErr == nil, platformCheck())
}
