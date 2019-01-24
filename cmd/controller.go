package cmd

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/bn2302/k8s-ha-controller-init/pkg"
	"github.com/spf13/cobra"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strconv"
	"time"
)

func createController() {
	exec.Command(
		"kubeadm",
		"init",
		"--config",
		clusterConfig["kubeadm-cfg-init.yaml"],
	).Run()
	exec.Command(
		"kubectl",
		"apply",
		"-f",
		"\"https://cloud.weave.works/k8s/net?k8s-version=$(kubectl version | base64 | tr -d '\n')\"",
	).Run()
	clusterInfoData, _ := exec.Command(
		"kubectl",
		"--kubeconfig=/etc/kubernetes/admin.conf",
		"get",
		"cm",
		"-n",
		"kube-public",
		"cluster-info",
		"-o",
		"jsonpath={.data.kubeconfig}",
	).Output()
	ioutil.WriteFile(clusterConfig["cluster-info.yaml"], clusterInfoData, 0644)
}

func joinController(apiDNS string, apiPort int) {
	exec.Command(
		"kubeadm",
		"join",
		apiDNS+":"+strconv.Itoa(apiPort),
		"--config",
		clusterConfig["kubeadm-cfg-join.yaml"],
		"--experimental-control-plane",
	).Run()
}

func deployController(apiDNS string, apiPort int, bucket string) {
	sess, err := session.NewSession()
	if err != nil {
		log.Fatalln("Could not initialize aws session: " + err.Error())
	} else {
		log.Println("AWS Session started")
	}
	metaSvc := ec2metadata.New(sess)
	instanceID, err := pkg.GetInstanceID(metaSvc)
	if err != nil {
		log.Fatalln("Could not get instance id: " + err.Error())
	} else {
		log.Println("Got instance ec2 instance id: " + instanceID)
	}

	region, err := pkg.GetRegion(metaSvc)
	if err != nil {
		log.Fatalln("Could not get the region: " + err.Error())
	} else {
		log.Println("Got the region: " + region)
	}
	autoSvc := autoscaling.New(sess, aws.NewConfig().WithRegion(region))
	s3Svc := s3.New(sess, aws.NewConfig().WithRegion(region))
	groupName, err := pkg.GetAutoscalingGroupName(autoSvc, instanceID)
	if err != nil {
		log.Fatalln("Could not get the autoscaling group name: " + err.Error())
	} else {
		log.Println("Got the autoscaling group name: " + groupName)
	}
	group, err := pkg.GetAutoscalingGroup(autoSvc, groupName)
	if err != nil {
		log.Fatalln("Could not get the autoscaling group : " + err.Error())
	}

	for {
		if !pkg.KubeUp(apiDNS, apiPort) {
			err = pkg.WaitTillCapacityReached(group, 600)
			if err != nil {
				log.Fatalln("Capacity of autoscaling group was not reached : " + err.Error())
			}
			if instanceID == pkg.GetAutoscalingInstances(group)[0] {
				if pkg.ExistsOnS3(s3Svc, bucket, &caKeys) {
					err := os.MkdirAll("/etc/kubernetes/pki/etcd", 0777)
					if err != nil {
						log.Fatalln("Could not create directory : " + err.Error())
					}
					pkg.DownloadMapFromS3(s3Svc, bucket, &caKeys)
				}
				pkg.DownloadFromS3(s3Svc, bucket, "kubeadm-cfg-init.yaml", clusterConfig["kubeadm-cfg-init.yaml"])
				createController()
				if !pkg.ExistsOnS3(s3Svc, bucket, &caKeys) {
					pkg.UploadMapToS3(s3Svc, bucket, &caKeys)
				}
				pkg.UploadToS3(s3Svc, bucket, "cluster-info.yaml", clusterConfig["cluster-info.yaml"])
				return
			}
		}
		if pkg.KubeUp(apiDNS, apiPort) && pkg.ExistsOnS3(s3Svc, bucket, &caKeys) {
			os.MkdirAll("/etc/kubernetes/pki/etcd", 0777)
			pkg.DownloadFromS3(s3Svc, bucket, "cluster-info.yaml", clusterConfig["cluster-info.yaml"])
			pkg.DownloadFromS3(s3Svc, bucket, "kubeadm-cfg-join.yaml", clusterConfig["kubeadm-cfg-join.yaml"])
			pkg.DownloadMapFromS3(s3Svc, bucket, &caKeys)
			joinController(apiDNS, apiPort)
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
		deployController(kubeAddress, kubePort, bucket)
	},
}

func init() {
	RootCmd.AddCommand(controllerCmd)
}
