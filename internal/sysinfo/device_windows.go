//go:build windows

package sysinfo

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// OSVersion is "10.0.26100".
func OSVersion() string {
	v := windows.RtlGetVersion()
	return fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
}

// DeviceModel is the computer's maker and model from the firmware ("LENOVO 20XW00A1RT").
func DeviceModel() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\BIOS`, registry.QUERY_VALUE)
	if err != nil {
		return "PC"
	}
	defer k.Close()
	maker, _, _ := k.GetStringValue("SystemManufacturer")
	model, _, _ := k.GetStringValue("SystemProductName")
	maker, model = strings.TrimSpace(maker), strings.TrimSpace(model)
	switch {
	case model == "":
		return "PC"
	case maker == "" || strings.HasPrefix(strings.ToLower(model), strings.ToLower(maker)):
		return model
	}
	return maker + " " + model
}

// MachineGUID is Windows' own id of this installation.
func MachineGUID() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, _ := k.GetStringValue("MachineGuid")
	return strings.TrimSpace(v)
}
