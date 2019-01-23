package cmd

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/s3"
	. "github.com/bn2302/k8s-ha-controller-init/pkg"
	"github.com/spf13/cobra"
	"os/exec"
	"strconv"
	"time"
)

var bucket string

func createController(token string) {
	exec.Command(
		"kubeadm",
		"init",
		"--config=kubeadm-config.yaml",
	).Run()
	exec.Command(
		"kubectl",
		"apply",
		"-f",
		"\"https://cloud.weave.works/k8s/net?k8s-version=$(kubectl version | base64 | tr -d '\n')\"",
	).Run()

}

func joinController(apiDNS string, apiPort int, token string) {
	exec.Command(
		"kubeadm",
		"join",
		apiDNS+":"+strconv.Itoa(apiPort),
		"--token "+token,
		"--experimental-control-plane",
	).Run()
}

func deployController(apiDNS string, apiPort int, bucket string, token string) {
	sess, _ := session.NewSession()
	metaSvc := ec2metadata.New(sess)
	instanceID, _ := GetInstanceID(metaSvc)
	region, _ := GetRegion(metaSvc)
	autoSvc := autoscaling.New(sess, aws.NewConfig().WithRegion(region))
	s3Svc := s3.New(sess, aws.NewConfig().WithRegion(region))
	groupName, _ := GetAutoscalingGroupName(autoSvc, instanceID)
	group, _ := GetAutoscalingGroup(autoSvc, groupName)
	for {
		if !KubeUp(apiDNS, apiPort) {
			_ = WaitTillCapacityReached(group, 600)
			if instanceID == GetAutoscalingInstances(group)[0] {
				if CaExistsOnS3(s3Svc, bucket) {
					DownloadCAFromS3(s3Svc, bucket)
				}
				createController(token)
				if !CaExistsOnS3(s3Svc, bucket) {
					UploadCAToS3(s3Svc, bucket)
				}
				return
			}
		}
		if KubeUp(apiDNS, apiPort) && CaExistsOnS3(s3Svc, bucket) {
			DownloadCAFromS3(s3Svc, bucket)
			joinController(apiDNS, apiPort, token)
			return
		}
		time.Sleep(time.Second * 1)
	}
}

var controllerCmd = &cobra.Command{
	Use:   "controller",
	Short: "Deploy controller",
	Long:  `Deploys a HA controller on AWS.`,
	Run: func(cmd *cobra.Command, args []string) {
		deployController(kubeAddress, kubePort, bucket, token)
	},
}

func init() {
	RootCmd.AddCommand(controllerCmd)
	controllerCmd.PersistentFlags().StringVarP(&bucket, "bucket", "b", "", "S3Bucket for the Kubernetes Config")
}
