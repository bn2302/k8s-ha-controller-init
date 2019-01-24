package cmd

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/bn2302/k8s-ha-controller-init/pkg"
	"github.com/spf13/cobra"
	"os/exec"
	"strconv"
)

func joinWorker(apiDNS string, apiPort int) {
	exec.Command(
		"kubeadm",
		"join",
		apiDNS+":"+strconv.Itoa(apiPort),
		"--config",
		clusterConfig["kubeadm-cfg-join.yaml"],
	).Run()
}

func deployWorker(apiDNS string, apiPort int) {
	sess, _ := session.NewSession()
	metaSvc := ec2metadata.New(sess)
	region, _ := pkg.GetRegion(metaSvc)
	s3Svc := s3.New(sess, aws.NewConfig().WithRegion(region))
	for {
		if pkg.KubeUp(apiDNS, apiPort) {
			pkg.DownloadFromS3(s3Svc, bucket, "cluster-info.yaml", clusterConfig["cluster-info.yaml"])
			pkg.DownloadFromS3(s3Svc, bucket, "kubeadm-cfg-join.yaml", clusterConfig["kubeadm-cfg-join.yaml"])
			joinWorker(apiDNS, apiPort)
			return
		}
	}
}

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Deploy worker",
	Long:  `Joins a worker.`,
	Run: func(cmd *cobra.Command, args []string) {
		deployWorker(kubeAddress, kubePort)
	},
}

func init() {
	RootCmd.AddCommand(workerCmd)

}
