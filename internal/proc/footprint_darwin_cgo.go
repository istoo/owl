//go:build darwin && cgo

package proc

/*
#include <mach/mach.h>
#include <mach/task_info.h>

// owl_task_footprint asks the kernel for this task's phys_footprint: the
// number Activity Monitor shows as Memory, which counts the webview and
// every other mapping, not only the Go heap.
static int owl_task_footprint(unsigned long long *out) {
	task_vm_info_data_t info;
	mach_msg_type_number_t count = TASK_VM_INFO_COUNT;
	kern_return_t kr = task_info(mach_task_self(), TASK_VM_INFO, (task_info_t)&info, &count);
	if (kr != KERN_SUCCESS) return (int)kr;
	*out = info.phys_footprint;
	return 0;
}
*/
import "C"

func currentFootprint() (uint64, string, bool) {
	var n C.ulonglong
	if C.owl_task_footprint(&n) != 0 {
		return 0, "", false
	}
	return uint64(n), "phys_footprint", true
}
