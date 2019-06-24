package pkg

import (
	"net"
	"testing"
)

func TestKubeUp(t *testing.T) {
	if KubeUp("", 6443) {
		t.Errorf("expect no error, tcp server should be down")
	}
	l, err := net.Listen("tcp", ":6443")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if !KubeUp("", 6443) {
		t.Errorf("expect no error, tcp server should be up")
	}
}
