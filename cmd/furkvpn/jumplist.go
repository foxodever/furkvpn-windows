//go:build windows

package main

import (
	"errors"
	"fmt"
	goruntime "runtime"
	"syscall"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

// The taskbar's tasks: a right-click on FurkVPN's taskbar button (also when it is pinned and the
// app is closed) offers "Подключить" and "Отключить", which start FurkVPN.exe --connect /
// --disconnect: the counterpart of the Android app's shortcuts.

var (
	clsidDestinationList            = ole.NewGUID("{77f10cf0-3db5-4966-b520-b7c54fd35ed6}")
	iidCustomDestinationList        = ole.NewGUID("{6332debf-87b5-4670-90c0-5e57b408a49e}")
	clsidEnumerableObjectCollection = ole.NewGUID("{2d3468c1-36a7-43b6-ac24-d3f02fd9607a}")
	iidObjectCollection             = ole.NewGUID("{5632b1a4-e38a-400a-928a-d4cd63230295}")
	iidObjectArray                  = ole.NewGUID("{92ca9dcd-5622-4bba-a805-5e9f541bd8c9}")
	clsidShellLink                  = ole.NewGUID("{00021401-0000-0000-C000-000000000046}")
	iidShellLinkW                   = ole.NewGUID("{000214F9-0000-0000-C000-000000000046}")
	iidPropertyStore                = ole.NewGUID("{886d8eeb-8cf2-4446-8d02-cdba1dbdcf99}")
	// PKEY_Title: what the jump list shows for a task.
	pkeyTitle = propertyKey{*ole.NewGUID("{F29F85E0-4FF9-1068-AB91-08002B27B3D9}"), 2}
)

// Method numbers in the interfaces' vtables (after IUnknown's three).
const (
	destListBeginList    = 4
	destListAddUserTasks = 7
	destListCommitList   = 8
	collAddObject        = 5
	linkSetDescription   = 7
	linkSetArguments     = 11
	linkSetIconLocation  = 17
	linkSetPath          = 20
	storeSetValue        = 6
	storeCommit          = 7
	vtLPWStr             = 31
)

type propertyKey struct {
	fmtid ole.GUID
	pid   uint32
}

// propVariant is PROPVARIANT holding a string (VT_LPWSTR).
type propVariant struct {
	vt       uint16
	reserved [3]uint16
	value    uintptr
	_        uintptr
}

type jumpTask struct{ title, args string }

// setTaskbarTasks replaces the taskbar's tasks of this app.
func setTaskbarTasks(exe string, tasks []jumpTask) error {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		var oleErr *ole.OleError
		if !errors.As(err, &oleErr) || oleErr.Code() != 1 { // S_FALSE: already initialized
			return err
		}
	}
	defer ole.CoUninitialize()

	list, err := ole.CreateInstance(clsidDestinationList, iidCustomDestinationList)
	if err != nil {
		return err
	}
	defer list.Release()
	var slots uint32
	var removed *ole.IUnknown
	if err := vcall(list, destListBeginList, uintptr(unsafe.Pointer(&slots)), uintptr(unsafe.Pointer(iidObjectArray)), uintptr(unsafe.Pointer(&removed))); err != nil {
		return fmt.Errorf("BeginList: %w", err)
	}
	if removed != nil {
		removed.Release()
	}
	collection, err := ole.CreateInstance(clsidEnumerableObjectCollection, iidObjectCollection)
	if err != nil {
		return err
	}
	defer collection.Release()
	for _, task := range tasks {
		if err := addTask(collection, exe, task); err != nil {
			return err
		}
	}
	array, err := collection.QueryInterface(iidObjectArray)
	if err != nil {
		return err
	}
	defer array.Release()
	if err := vcall(list, destListAddUserTasks, uintptr(unsafe.Pointer(array))); err != nil {
		return fmt.Errorf("AddUserTasks: %w", err)
	}
	if err := vcall(list, destListCommitList); err != nil {
		return fmt.Errorf("CommitList: %w", err)
	}
	return nil
}

func addTask(collection *ole.IUnknown, exe string, task jumpTask) error {
	link, err := ole.CreateInstance(clsidShellLink, iidShellLinkW)
	if err != nil {
		return err
	}
	defer link.Release()
	path, _ := windows.UTF16PtrFromString(exe)
	args, _ := windows.UTF16PtrFromString(task.args)
	title, _ := windows.UTF16PtrFromString(task.title)
	for _, step := range []struct {
		method int
		args   []uintptr
	}{
		{linkSetPath, []uintptr{uintptr(unsafe.Pointer(path))}},
		{linkSetArguments, []uintptr{uintptr(unsafe.Pointer(args))}},
		{linkSetIconLocation, []uintptr{uintptr(unsafe.Pointer(path)), 0}},
		{linkSetDescription, []uintptr{uintptr(unsafe.Pointer(title))}},
	} {
		if err := vcall(link, step.method, step.args...); err != nil {
			return err
		}
	}
	store, err := link.QueryInterface(iidPropertyStore)
	if err != nil {
		return err
	}
	defer store.Release()
	value := propVariant{vt: vtLPWStr, value: uintptr(unsafe.Pointer(title))}
	if err := vcall(&store.IUnknown, storeSetValue, uintptr(unsafe.Pointer(&pkeyTitle)), uintptr(unsafe.Pointer(&value))); err != nil {
		return fmt.Errorf("title: %w", err)
	}
	if err := vcall(&store.IUnknown, storeCommit); err != nil {
		return err
	}
	err = vcall(collection, collAddObject, uintptr(unsafe.Pointer(link)))
	goruntime.KeepAlive(path)
	goruntime.KeepAlive(args)
	goruntime.KeepAlive(title)
	goruntime.KeepAlive(&value)
	return err
}

// vcall calls method number index of a COM object; an HRESULT below zero is an error.
func vcall(obj *ole.IUnknown, index int, args ...uintptr) error {
	vtable := *(*[32]uintptr)(unsafe.Pointer(obj.RawVTable))
	hr, _, _ := syscall.SyscallN(vtable[index], append([]uintptr{uintptr(unsafe.Pointer(obj))}, args...)...)
	if int32(hr) < 0 {
		return ole.NewError(hr)
	}
	return nil
}
