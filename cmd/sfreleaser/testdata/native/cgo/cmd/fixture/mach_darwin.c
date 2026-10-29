// Includes the SDK header prometheus/client_golang's cgo memory collector needs, which zig lacks.
#include <mach/mach.h>
#include <mach/mach_vm.h>

int resident_regions(void) {
	mach_vm_address_t address = 0;
	mach_vm_size_t size = 0;
	vm_region_basic_info_data_64_t info;
	mach_msg_type_number_t count = VM_REGION_BASIC_INFO_COUNT_64;
	mach_port_t object;
	return mach_vm_region(mach_task_self(), &address, &size, VM_REGION_BASIC_INFO_64, (vm_region_info_t)&info, &count, &object) == KERN_SUCCESS;
}
