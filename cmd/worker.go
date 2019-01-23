package cmd

import (
	. "github.com/bn2302/k8s-ha-controller-init/pkg"
	"github.com/spf13/cobra"
	"os/exec"
	"strconv"
)

func joinWorker(apiDNS string, apiPort int, token string) {
	exec.Command(
		"kubeadm",
		"join",
		apiDNS+":"+strconv.Itoa(apiPort),
		"--token "+token,
	).Run()
}

func deployWorker(apiDNS string, apiPort int, token string) {
	for {
		if KubeUp(apiDNS, apiPort) {
			joinWorker(apiDNS, apiPort, token)
			return
		}
	}
}

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Deploy worker",
	Long:  `Joins a worker.`,
	Run: func(cmd *cobra.Command, args []string) {
		deployWorker(kubeAddress, kubePort, token)
	},
}

func init() {
	RootCmd.AddCommand(workerCmd)

}
