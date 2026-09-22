package app_interface

import (
	"fmt"

	"github.com/fxamacker/cbor"
)

var osFunctions *AppFunctions = nil

func SetOSFunctions(appInterface AppInterface) {
	osFunctions = &AppFunctions{javaInterface: appInterface}
}

func HasOSFunctions() bool {
	return osFunctions != nil
}

func GetNetworkInterfaces() ([]NetworkInterface, error) {
	if osFunctions == nil {
		return nil, fmt.Errorf("Android OS functions are not initialized")
	}

	bytes, err := osFunctions.call("GetNetworkInterfaces", nil)
	if err != nil {
		return nil, err
	}
	var interfaces []NetworkInterface
	err = cbor.Unmarshal(bytes, &interfaces)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response from java: %s", err)
	}
	for _, i := range interfaces {
		i.setFlagsValue()
	}
	return interfaces, nil
}

func GetNetworkAddresses() ([]NetworkAddress, error) {
	if osFunctions == nil {
		return nil, fmt.Errorf("Android OS functions are not initialized")
	}

	bytes, err := osFunctions.call("GetNetworkAddresses", nil)
	if err != nil {
		return nil, err
	}
	var addresses []NetworkAddress
	err = cbor.Unmarshal(bytes, &addresses)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response from java: %s", err)
	}
	return addresses, nil
}

func GetPlatformInfo() (*PlatformInfo, error) {
	if osFunctions == nil {
		return nil, fmt.Errorf("Android OS functions are not initialized")
	}

	info := &PlatformInfo{}

	bytes, err := osFunctions.call("GetPlatformInfo", nil)
	if err != nil {
		return nil, err
	}
	err = cbor.Unmarshal(bytes, info)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cbor: %s", err)
	}

	return info, nil
}

func Shutdown() error {
	if osFunctions == nil {
		return fmt.Errorf("Android OS functions are not initialized")
	}

	_, err := osFunctions.call("Shutdown", nil)
	if err != nil {
		return err
	}

	return nil
}
