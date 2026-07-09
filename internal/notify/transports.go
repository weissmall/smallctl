package notify

import (
	"fmt"
	"os/exec"
)

func notifySendTransport(title, body string) error {
	cmd := exec.Command("notify-send", title, body)
	return cmd.Run()
}

func gdbusTransport(title, body string) error {
	cmd := exec.Command("gdbus", "call", "--session",
		"--dest", "org.freedesktop.Notifications",
		"--object-path", "/org/freedesktop/Notifications",
		"--method", "org.freedesktop.Notifications.Notify",
		"smallctl",
		"0",
		"",
		title,
		body,
		"[]",
		"{}",
		"5000",
	)
	return cmd.Run()
}

func dbusSendTransport(title, body string) error {
	cmd := exec.Command("dbus-send", "--session",
		"--dest=org.freedesktop.Notifications",
		"--type=method_call",
		"--print-reply",
		"/org/freedesktop/Notifications",
		"org.freedesktop.Notifications.Notify",
		"string:smallctl",
		"uint32:0",
		"string:",
		fmt.Sprintf("string:%s", title),
		fmt.Sprintf("string:%s", body),
		"array:string:",
		"dict:string:string:",
		"int32:5000",
	)
	return cmd.Run()
}
