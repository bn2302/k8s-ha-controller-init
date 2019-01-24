package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"log"
	"os"
)

var kubeAddress string
var kubePort int
var token string
var bucket string

var caKeys = map[string]string{
	"admin.conf":         "/etc/kubernetes/admin.conf",
	"ca.crt":             "/etc/kubernetes/pki/ca.crt",
	"ca.key":             "/etc/kubernetes/pki/ca.key",
	"etcd-ca.crt":        "/etc/kubernetes/pki/etcd/ca.crt",
	"etcd-ca.key":        "/etc/kubernetes/pki/etcd/ca.key",
	"front-proxy-ca.crt": "/etc/kubernetes/pki/front-proxy-ca.crt",
	"front-proxy-ca.key": "/etc/kubernetes/pki/front-proxy-ca.key",
	"sa.key":             "/etc/kubernetes/pki/sa.key",
	"sa.pub":             "/etc/kubernetes/pki/sa.pub",
}

var clusterConfig = map[string]string{
	"cluster-info.yaml":     "/tmp/cluster-info.yaml",
	"kubeadm-cfg-init.yaml": "/tmp/cluster-cfg.yaml",
	"kubeadm-cfg-join.yaml": "/tmp/cluster-join.yaml",
}

//RootCmd is the entry point to the application
var RootCmd = &cobra.Command{
	Use:           "k8sinit",
	Short:         "Deploy a HA kubernetes",
	Long:          `Initialize a kubernetes HA cluster using kubeadm on AWS`,
	SilenceErrors: true,
}

//Execute starts the root cmd
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	log.SetOutput(os.Stdout)
	RootCmd.PersistentFlags().StringVarP(&kubeAddress, "name", "n", "", "Address of the Kubernetes API Server")
	RootCmd.PersistentFlags().IntVarP(&kubePort, "port", "p", 6443, "Port of the Kubernetes API Server")
	RootCmd.PersistentFlags().StringVarP(&bucket, "bucket", "b", "", "S3Bucket for the Kubernetes Config")
}
