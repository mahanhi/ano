package via

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbar/hbar/internal/store"
)

func TestRunSteerStaysOff(t *testing.T) {
	err := RunSteer("does-not-exist.json")
	if err == nil || !strings.Contains(err.Error(), "restarted the Mac") {
		t.Fatal(err)
	}
}

func TestBackgroundDoesNotUseNohup(t *testing.T) {
	got := background("'/tmp/hbar-steer' '/tmp/via-steer.json'", "/tmp/via steer.log")
	if strings.Contains(got, "nohup") || !strings.Contains(got, "trap '' HUP") || !strings.Contains(got, "'/tmp/via steer.log'") {
		t.Fatal(got)
	}
}

func TestAddedDevicesIncludesEveryDeviceOnThePath(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.ConnectRouter("192.168.86.1", "Wi-Fi", []store.LANNeighbor{
		{Name: "Maharajan's Fire TV", Addr: "192.168.86.57", MAC: "90:39:5f:ad:ac:4b", Kind: "tv"},
		{Name: "XBR-49X830C", Addr: "192.168.86.220", MAC: "60:6d:c7:a7:b7:28", Kind: "tv"},
		{Name: "Hall phone", Addr: "192.168.86.40", MAC: "66:55:44:33:22:11", Kind: "phone"},
	}); err != nil {
		t.Fatal(err)
	}
	devices, err := st.Devices()
	if err != nil {
		t.Fatal(err)
	}
	for _, device := range devices {
		if device.Addr == "192.168.86.40" {
			continue
		}
		if err := st.SetViaAno(device.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	got, err := addedDevices(st, "192.168.86.192", "192.168.86.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("devices %+v", got)
	}
	seen := map[string]string{}
	for _, device := range got {
		seen[device.Addr] = device.MAC
	}
	if seen["192.168.86.57"] != "90:39:5f:ad:ac:4b" || seen["192.168.86.220"] != "60:6d:c7:a7:b7:28" {
		t.Fatalf("devices %+v", got)
	}
	if _, ok := seen["192.168.86.40"]; ok {
		t.Fatal("a device that was not added was included")
	}
}
