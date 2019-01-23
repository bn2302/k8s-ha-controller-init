package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
)

var kubeAddress string
var kubePort int
var token string

var RootCmd = &cobra.Command{
	Use:           "k8sinit",
	Short:         "Deploy a HA kubernetes",
	Long:          `Initialize a kubernetes HA cluster using kubeadm on AWS`,
	SilenceErrors: true,
}

func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	RootCmd.PersistentFlags().StringVarP(&kubeAddress, "name", "n", "", "Address of the Kubernetes API Server")
	RootCmd.PersistentFlags().IntVarP(&kubePort, "port", "p", 6443, "Port of the Kubernetes API Server")
	RootCmd.PersistentFlags().StringVarP(&token, "token", "t", "", "Token for bootstrapping the control plane")
}
