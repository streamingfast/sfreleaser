// References the unwinder functions that GCC's runtime provides and that prebuilt Rust libraries,
// such as wasmtime-go's, call. zig only links them with -lunwind.
struct _Unwind_Context;
typedef int (*_Unwind_Trace_Fn)(struct _Unwind_Context *, void *);

extern void __register_frame(void *);
extern void __deregister_frame(void *);
extern int _Unwind_Backtrace(_Unwind_Trace_Fn, void *);

// Volatile so that the compiler keeps the references instead of folding the check away.
void *volatile unwind_functions[] = {(void *)__register_frame, (void *)__deregister_frame, (void *)_Unwind_Backtrace};

int has_register_frame(void) {
	return unwind_functions[0] != 0 && unwind_functions[1] != 0 && unwind_functions[2] != 0;
}
