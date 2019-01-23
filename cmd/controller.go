package cmd

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/bn2302/k8s-ha-controller-init/pkg"
	"github.com/spf13/cobra"
	"os"
	"os/exec"
	"strconv"
	"time"
)

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
	instanceID, _ := pkg.GetInstanceID(metaSvc)
	region, _ := pkg.GetRegion(metaSvc)
	autoSvc := autoscaling.New(sess, aws.NewConfig().WithRegion(region))
	s3Svc := s3.New(sess, aws.NewConfig().WithRegion(region))
	groupName, _ := pkg.GetAutoscalingGroupName(autoSvc, instanceID)
	group, _ := pkg.GetAutoscalingGroup(autoSvc, groupName)
	for {
		if !pkg.KubeUp(apiDNS, apiPort) {
			_ = pkg.WaitTillCapacityReached(group, 600)
			if instanceID == pkg.GetAutoscalingInstances(group)[0] {
				if pkg.ExistsOnS3(s3Svc, bucket, &caKeys) {
					os.MkdirAll("/etc/kubernetes/pki/etcd", 0777)
					pkg.DownloadFromS3(s3Svc, bucket, &caKeys)
				}
				createController(token)
				if !pkg.ExistsOnS3(s3Svc, bucket, &caKeys) {
					pkg.UploadToS3(s3Svc, bucket, &caKeys)
				}
				return
			}
		}
		if pkg.KubeUp(apiDNS, apiPort) && pkg.ExistsOnS3(s3Svc, bucket, &caKeys) {
			os.MkdirAll("/etc/kubernetes/pki/etcd", 0777)
			pkg.DownloadFromS3(s3Svc, bucket, &caKeys)
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
