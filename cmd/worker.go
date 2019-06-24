package cmd

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/bn2302/k8s-ha-controller-init/pkg"
	"github.com/spf13/cobra"
	"log"
	"os/exec"
	"strconv"
)

func joinWorker(apiDNS string, apiPort int) {
	joinCmd := exec.Command(
		"kubeadm",
		"join",
		apiDNS+":"+strconv.Itoa(apiPort),
		"--config",
		clusterConfig["kubeadm-cfg-join.yaml"],
	)

	if err := joinCmd.Run(); err != nil {
		log.Fatalln("Failed to join worker: " + err.Error())
	}
}

func deployWorker(apiDNS string, apiPort int) {
	sess, err := session.NewSession()
	if err != nil {
		log.Fatalln("Could not initialize aws session: " + err.Error())
	} else {
		log.Println("AWS Session started")
	}
	metaSvc := ec2metadata.New(sess)

	region, err := pkg.GetRegion(metaSvc)
	if err != nil {
		log.Fatalln("Could not get the region: " + err.Error())
	} else {
		log.Println("Got the region: " + region)
	}
	s3Svc := s3.New(sess, aws.NewConfig().WithRegion(region))

	log.Println("Wait till DNS resolves")
	pkg.DNSResolves(apiDNS)
	log.Println("Start deployment loop")
	for {
		if pkg.KubeUp(apiDNS, apiPort) {
			for {
				if err := pkg.DownloadFromS3(s3Svc, bucket, "cluster-info.yaml", clusterConfig["cluster-info.yaml"]); err == nil {
					break
				}
			}
			for {
				if err := pkg.DownloadFromS3(s3Svc, bucket, "kubeadm-cfg-join.yaml", clusterConfig["kubeadm-cfg-join.yaml"]); err == nil {
					break
				}
			}
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
