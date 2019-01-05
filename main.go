package main

import (
	"fmt"
	"os"

	"github.com/bn2302/k8s-ha-controller-init/cmd"
)

func main() {
	if err := cmd.NewHAKubeadm(os.Stdout).Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		os.Exit(1)
	}
}
