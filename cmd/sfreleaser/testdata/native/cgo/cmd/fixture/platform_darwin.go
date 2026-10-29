package main

/*
int resident_regions(void);
*/
import "C"

func platformCheck() bool { return C.resident_regions() == 1 }
